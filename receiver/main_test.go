package main

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T) *receiver {
	t.Helper()
	d := t.TempDir()
	tmp := t.TempDir()
	root, err := os.OpenRoot(d)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	s := &receiver{shareAll: true, token: "test-session", url: "http://192.168.1.2:8080/", dest: d, temp: tmp, root: root, sessions: map[string]string{"test-cookie": "test-csrf"}}
	s.rotatePIN()
	return s
}
func post(t *testing.T, s *receiver, name string, data []byte, origins ...string) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, err := mw.CreateFormFile("book", name)
	if err != nil {
		t.Fatal(err)
	}
	part.Write(data)
	mw.Close()
	r := httptest.NewRequest("POST", "http://kobo/api/upload", &body)
	r.RemoteAddr = "192.168.1.3:1234"
	r.Header.Set("Content-Type", mw.FormDataContentType())
	r.Header.Set("Cookie", "kobo_session=test-cookie")
	r.Header.Set("X-CSRF-Token", "test-csrf")
	if len(origins) > 0 {
		r.Header.Set("Origin", origins[0])
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}
func TestUploadAndDuplicate(t *testing.T) {
	s := fixture(t)
	pdf := []byte("%PDF-1.7\ntest book")
	for i := 0; i < 2; i++ {
		w := post(t, s, "book.pdf", pdf)
		if w.Code != 200 {
			t.Fatalf("%d: %s", w.Code, w.Body)
		}
	}
	for _, name := range []string{"book.pdf", "book (1).pdf"} {
		b, e := os.ReadFile(filepath.Join(s.dest, name))
		if e != nil || !bytes.Equal(b, pdf) {
			t.Fatalf("%s: %v", name, e)
		}
	}
	entries, _ := os.ReadDir(s.temp)
	if len(entries) != 0 {
		t.Fatal("temporary uploads leaked")
	}
}
func TestEPUB(t *testing.T) {
	s := fixture(t)
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	f, _ := z.Create("mimetype")
	io.WriteString(f, "application/epub+zip")
	z.Close()
	w := post(t, s, "story.epub", b.Bytes())
	if w.Code != 200 {
		t.Fatalf("%d: %s", w.Code, w.Body)
	}
}
func TestRejectInvalidFiles(t *testing.T) {
	for _, name := range []string{"script.sh", "book.pdf", "book.epub"} {
		t.Run(name, func(t *testing.T) {
			s := fixture(t)
			w := post(t, s, name, []byte("not a book"))
			if w.Code != 400 {
				t.Fatal(w.Code)
			}
			entries, _ := os.ReadDir(s.dest)
			if len(entries) != 0 {
				t.Fatal("invalid file saved")
			}
		})
	}
}
func TestCapabilityAndLocalOnlyScreen(t *testing.T) {
	s := fixture(t)
	for _, path := range []string{"/screen", "/qr.png", "/health", "/wrong-token/"} {
		r := httptest.NewRequest("GET", "http://kobo"+path, nil)
		r.RemoteAddr = "192.168.1.3:1234"
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != 404 {
			t.Fatalf("%s: %d", path, w.Code)
		}
	}
	r := httptest.NewRequest("GET", "http://127.0.0.1:8080/screen", nil)
	r.RemoteAddr = "127.0.0.1:1234"
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), s.url) {
		t.Fatal("missing QR screen")
	}
}
func TestFilenameSafety(t *testing.T) {
	for _, input := range []string{"../../book.pdf", `..\..\book.pdf`} {
		name, e := cleanName(input)
		if e != nil || name != "book.pdf" {
			t.Fatalf("%q %v", name, e)
		}
	}
}
func TestCrossOriginRejected(t *testing.T) {
	s := fixture(t)
	r := httptest.NewRequest("POST", "http://kobo/api/upload", strings.NewReader(""))
	r.Header.Set("Origin", "http://other")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
}

func TestBrowserFormOrigins(t *testing.T) {
	for _, origin := range []string{"", "null", "http://kobo"} {
		t.Run(origin, func(t *testing.T) {
			s := fixture(t)
			w := post(t, s, "safari.pdf", []byte("%PDF-1.7\nphone upload"), origin)
			if w.Code != 200 {
				t.Fatalf("origin %q: %d %s", origin, w.Code, w.Body)
			}
		})
	}
}

func TestOpaqueCrossSiteRejected(t *testing.T) {
	s := fixture(t)
	r := httptest.NewRequest("POST", "http://kobo/api/upload", strings.NewReader(""))
	r.Header.Set("Origin", "null")
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
}

func TestFinishOnlyFromKoboWithToken(t *testing.T) {
	for _, tc := range []struct {
		name, remote, token string
		want                int
	}{
		{"phone cannot stop receiver", "192.168.1.3:1234", "test-session", 404},
		{"wrong token", "127.0.0.1:1234", "wrong", 403},
		{"missing token", "127.0.0.1:1234", "", 403},
		{"local button", "127.0.0.1:1234", "test-session", 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := fixture(t)
			called := false
			s.onFinish = func() error { called = true; return nil }
			r := httptest.NewRequest("POST", "http://127.0.0.1:8080/finish", strings.NewReader("token="+tc.token))
			r.RemoteAddr = tc.remote
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			// The embedded Kobo browser can retain an origin from its native shell.
			r.Header.Set("Origin", "http://www.kobo.com")
			r.Header.Set("Sec-Fetch-Site", "cross-site")
			w := httptest.NewRecorder()
			s.ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatalf("%d: %s", w.Code, w.Body)
			}
			if called != (tc.want == 200) {
				t.Fatal("finish callback authorization mismatch")
			}
			if called {
				w = post(t, s, "late.pdf", []byte("%PDF-1.7\nlate"))
				if w.Code != 503 {
					t.Fatal("new upload accepted after finish")
				}
			}
		})
	}
}

func TestFinishHelperFailureKeepsReceiving(t *testing.T) {
	s := fixture(t)
	s.onFinish = func() error { return fmt.Errorf("helper missing") }
	r := httptest.NewRequest("POST", "http://127.0.0.1:8080/finish", strings.NewReader("token="+s.token))
	r.RemoteAddr = "127.0.0.1:1234"
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if !strings.Contains(w.Body.String(), "Import helper unavailable") {
		t.Fatal(w.Body)
	}
	w = post(t, s, "still-works.pdf", []byte("%PDF-1.7\nstill receiving"))
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
}

func TestLibraryScanBeforeServerShutdown(t *testing.T) {
	finished := make(chan struct{}, 1)
	called := false
	err := requestLibraryScan(func(method string) error {
		if method != "pfmRescanBooks" {
			t.Fatal(method)
		}
		select {
		case <-finished:
			t.Fatal("server shutdown requested before scan")
		default:
		}
		called = true
		return nil
	}, finished)
	if err != nil || !called {
		t.Fatal("scan not requested", err)
	}
	select {
	case <-finished:
	default:
		t.Fatal("shutdown not requested after successful scan")
	}
	err = requestLibraryScan(func(string) error { return fmt.Errorf("scan failed") }, finished)
	if err == nil {
		t.Fatal("scan error was hidden")
	}
	select {
	case <-finished:
		t.Fatal("shutdown requested after failed scan")
	default:
	}
}

func TestConcurrentFinishWaitsForResult(t *testing.T) {
	s := fixture(t)
	release := make(chan struct{})
	calls := 0
	s.onFinish = func() error {
		calls++
		<-release
		return fmt.Errorf("helper missing")
	}
	first := make(chan error, 1)
	go func() { first <- s.finishTransfers() }()
	for {
		s.mu.Lock()
		started := s.finishing
		s.mu.Unlock()
		if started {
			break
		}
	}
	second := make(chan error, 1)
	go func() { second <- s.finishTransfers() }()
	close(release)
	if e := <-first; e == nil {
		t.Fatal("first finish hid the helper failure")
	}
	if e := <-second; e == nil {
		t.Fatal("concurrent finish reported success although the scan failed")
	}
	if calls != 2 {
		t.Fatalf("library scan attempted %d times, want 2", calls)
	}
	s.onFinish = func() error { return nil }
	if s.finishTransfers() != nil || s.finishTransfers() != nil {
		t.Fatal("finish after success should report success")
	}
}

func basicUpload(t *testing.T, s *receiver, csrf string) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if csrf != "" {
		mw.WriteField("csrf", csrf)
	}
	part, _ := mw.CreateFormFile("book", "basic.pdf")
	part.Write([]byte("%PDF-1.7\nbasic"))
	mw.Close()
	r := httptest.NewRequest("POST", "http://kobo/upload", &body)
	r.RemoteAddr = "192.168.1.3:1234"
	r.Header.Set("Content-Type", mw.FormDataContentType())
	r.Header.Set("Cookie", "kobo_session=test-cookie")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}

func TestBasicUploadTokenInForm(t *testing.T) {
	s := fixture(t)
	for _, csrf := range []string{"", "wrong"} {
		if w := basicUpload(t, s, csrf); w.Code != 403 {
			t.Fatalf("csrf %q: %d %s", csrf, w.Code, w.Body)
		}
	}
	if w := basicUpload(t, s, "test-csrf"); w.Code != 200 {
		t.Fatalf("%d: %s", w.Code, w.Body)
	}
	if _, e := os.Stat(filepath.Join(s.dest, "basic.pdf")); e != nil {
		t.Fatal(e)
	}
}

func TestSaveLeavesNoStagedFiles(t *testing.T) {
	s := fixture(t)
	if w := post(t, s, "staged.pdf", []byte("%PDF-1.7\nstaged")); w.Code != 200 {
		t.Fatalf("%d: %s", w.Code, w.Body)
	}
	entries, _ := os.ReadDir(s.dest)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".receiving-") {
			t.Fatal("staged upload leaked:", e.Name())
		}
	}
	// A name reserved by an in-progress save is empty and must not be listed.
	os.WriteFile(filepath.Join(s.dest, "reserved.pdf"), nil, 0644)
	files, e := s.allBooks(httptest.NewRequest("GET", "/", nil))
	if e != nil {
		t.Fatal(e)
	}
	for _, f := range files {
		if strings.HasSuffix(f.Path, "reserved.pdf") {
			t.Fatal("empty reserved file listed")
		}
	}
}
