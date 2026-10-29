package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func call(s *receiver, method, target, body string, auth bool) *httptest.ResponseRecorder {
	if target == "/pair" && method == "POST" {
		body += "&nonce=testnonce"
	}
	r := httptest.NewRequest(method, "http://kobo"+target, strings.NewReader(body))
	if target == "/pair" {
		r.Header.Set("Cookie", "kobo_pair=testnonce")
	}
	r.RemoteAddr = "192.168.1.3:1234"
	if auth {
		r.Header.Set("Cookie", "kobo_session=test-cookie")
	}
	if method == "POST" {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}
func seed(t *testing.T, s *receiver, p string) []byte {
	t.Helper()
	data := []byte("%PDF-1.7\n" + p)
	target := filepath.Join(s.dest, filepath.FromSlash(p))
	if e := os.MkdirAll(filepath.Dir(target), 0755); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(target, data, 0644); e != nil {
		t.Fatal(e)
	}
	return data
}
func TestPairingAndSession(t *testing.T) {
	s := fixture(t)
	w := call(s, "GET", "/", "", false)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Pairing code") {
		t.Fatal(w.Code, w.Body)
	}
	if w := call(s, "GET", "/api/files", "", false); w.Code != 401 {
		t.Fatal("unauthorized file access", w.Code)
	}
	w = call(s, "POST", "/pair", "code="+s.pin, false)
	if w.Code != 303 {
		t.Fatal(w.Code, w.Body)
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatal("insecure session cookie")
	}
	r := httptest.NewRequest("GET", "http://kobo/api/session", nil)
	r.AddCookie(cookies[0])
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "csrf") {
		t.Fatal(w.Code, w.Body)
	}
	if w = call(s, "POST", "/zip", "file=book.pdf", true); w.Code != 403 {
		t.Fatal("missing csrf accepted", w.Code)
	}
	if w = call(s, "GET", "/test-session/", "", false); w.Code != 404 {
		t.Fatal("old URL still active")
	}
}
func TestPairExpiryLockoutAndRateLimit(t *testing.T) {
	s := fixture(t)
	s.pinUntil = time.Now().Add(-time.Second)
	if w := call(s, "POST", "/pair", "code="+s.pin, false); w.Code != 429 {
		t.Fatal(w.Code)
	}
	s.rotatePIN()
	for i := 0; i < 10; i++ {
		s.lastPair = time.Time{}
		bad := "000000"
		if bad == s.pin {
			bad = "000001"
		}
		if w := call(s, "POST", "/pair", "code="+bad, false); w.Code != 403 {
			t.Fatal(w.Code)
		}
	}
	s.lastPair = time.Time{}
	if w := call(s, "POST", "/pair", "code="+s.pin, false); w.Code != 429 {
		t.Fatal("attempt budget bypassed", w.Code)
	}
	s.rotatePIN()
	s.lastPair = time.Now()
	if w := call(s, "POST", "/pair", "code="+s.pin, false); w.Code != 429 {
		t.Fatal("rate limit bypassed", w.Code)
	}
}
func TestLibraryAndDownloads(t *testing.T) {
	s := fixture(t)
	data := seed(t, s, "Books/日本語 & notes.pdf")
	seed(t, s, ".adds/private.pdf")
	seed(t, s, ".kobo/private.pdf")
	seed(t, s, "System Volume Information/private.pdf")
	seed(t, s, "Books/.hidden.pdf")
	os.WriteFile(filepath.Join(s.dest, "other.txt"), []byte("other"), 0644)
	w := call(s, "GET", "/api/files", "", true)
	var files []book
	if e := json.Unmarshal(w.Body.Bytes(), &files); e != nil {
		t.Fatal(e, w.Body)
	}
	if len(files) != 1 || files[0].Path != "Books/日本語 & notes.pdf" {
		t.Fatal(files)
	}
	w = call(s, "GET", "/download?file="+url.QueryEscape(files[0].Path), "", true)
	if w.Code != 200 || !bytes.Equal(w.Body.Bytes(), data) || !strings.Contains(w.Header().Get("Content-Disposition"), "attachment") {
		t.Fatal(w.Code, w.Body)
	}
	for _, p := range []string{"../outside.pdf", "/absolute.pdf", "Books/../.adds/private.pdf", ".adds/private.pdf", `Books\x.pdf`, "other.txt"} {
		if w := call(s, "GET", "/download?file="+url.QueryEscape(p), "", true); w.Code != 404 {
			t.Fatal(p, w.Code)
		}
	}
	r := httptest.NewRequest("GET", "http://kobo/download?file="+url.QueryEscape(files[0].Path), nil)
	r.Header.Set("Cookie", "kobo_session=test-cookie")
	r.Header.Set("Range", "bytes=0-4")
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 206 || w.Body.String() != "%PDF-" {
		t.Fatal("range failed", w.Code, w.Body)
	}
}
func TestSymlinksHidden(t *testing.T) {
	s := fixture(t)
	outside := filepath.Join(t.TempDir(), "secret.pdf")
	os.WriteFile(outside, []byte("secret"), 0644)
	if e := os.Symlink(outside, filepath.Join(s.dest, "link.pdf")); e != nil {
		t.Skip("symlink creation unavailable:", e)
	}
	if w := call(s, "GET", "/download?file=link.pdf", "", true); w.Code != 404 {
		t.Fatal("symlink exposed", w.Code)
	}
	w := call(s, "GET", "/api/files", "", true)
	if w.Body.String() != "[]\n" {
		t.Fatal(w.Body)
	}
}
func TestStreamZIPAndBasicReview(t *testing.T) {
	s := fixture(t)
	a := seed(t, s, "One/book.pdf")
	b := seed(t, s, "Two/book.pdf")
	form := url.Values{"csrf": {"test-csrf"}, "file": {"One/book.pdf", "Two/book.pdf", "One/book.pdf"}}.Encode()
	w := call(s, "POST", "/review", form, true)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "2 books selected") || !strings.Contains(w.Body.String(), "/One/book.pdf") {
		t.Fatal(w.Code, w.Body)
	}
	w = call(s, "POST", "/zip", form, true)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	z, e := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
	if e != nil {
		t.Fatal(e)
	}
	if len(z.File) != 2 {
		t.Fatal(len(z.File))
	}
	for i, f := range z.File {
		if f.Method != zip.Store {
			t.Fatal("archive recompressed")
		}
		r, e := f.Open()
		if e != nil {
			t.Fatal(e)
		}
		got, e := io.ReadAll(r)
		r.Close()
		if e != nil || !bytes.Equal(got, [][]byte{a, b}[i]) {
			t.Fatal("ZIP bytes differ", e)
		}
	}
	w = call(s, "POST", "/zip", form+"&file=missing.pdf", true)
	if w.Code != 400 || w.Header().Get("Content-Type") == "application/zip" {
		t.Fatal("failed selection started ZIP")
	}
	w = call(s, "GET", "/basic", "", true)
	if w.Code != 200 || strings.Contains(w.Body.String(), "<script") || !strings.Contains(w.Body.String(), "One/book.pdf") {
		t.Fatal(w.Code, w.Body)
	}
}
func TestBusyAndLimits(t *testing.T) {
	s := fixture(t)
	s.transferMu.Lock()
	w := post(t, s, "busy.pdf", []byte("%PDF-1.7\n"))
	s.transferMu.Unlock()
	if w.Code != 409 {
		t.Fatal(w.Code)
	}
	seed(t, s, "a.pdf")
	form := url.Values{"csrf": {"test-csrf"}}
	for i := 0; i < 101; i++ {
		form.Add("file", "a.pdf")
	}
	if w := call(s, "POST", "/zip", form.Encode(), true); w.Code != 400 {
		t.Fatal(w.Code)
	}
}

func TestNativeBrowserOriginWithTokens(t *testing.T) {
	s := fixture(t)
	seed(t, s, "native.pdf")
	r := httptest.NewRequest("POST", "http://kobo/review", strings.NewReader("csrf=test-csrf&file=native.pdf"))
	r.Header.Set("Cookie", "kobo_session=test-cookie")
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Origin", "http://www.kobo.com")
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal("valid native form rejected", w.Code, w.Body)
	}
	r = httptest.NewRequest("POST", "http://kobo/pair", strings.NewReader("code="+s.pin+"&nonce=challenge"))
	r.Header.Set("Cookie", "kobo_pair=challenge")
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Origin", "http://www.kobo.com")
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 303 {
		t.Fatal("valid native pairing rejected", w.Code, w.Body)
	}
}
func TestPairChallengeAndHost(t *testing.T) {
	s := fixture(t)
	r := httptest.NewRequest("POST", "http://kobo/pair", strings.NewReader("code="+s.pin))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("missing challenge accepted", w.Code)
	}
	s.allowedHosts = map[string]bool{"kobo": true}
	r = httptest.NewRequest("GET", "http://attacker.example/screen", nil)
	r.RemoteAddr = "127.0.0.1:1234"
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("rebound host accepted", w.Code)
	}
	s.lastPair = time.Time{}
	wrong := "000000"
	if wrong == s.pin {
		wrong = "000001"
	}
	w = call(s, "POST", "/pair", "code="+wrong, false)
	if w.Code != 403 || len(w.Result().Cookies()) != 1 {
		t.Fatal("pairing retry challenge missing", w.Code, w.Header())
	}
}
