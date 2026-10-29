package main

import (
	"bytes"
	"encoding/json"
	qrcode "github.com/skip2/go-qrcode"
	"io"
	"mime/multipart"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func tracked(t *testing.T, s *receiver, id, name string, data []byte, metadataSize string) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	m := multipart.NewWriter(&body)
	p, e := m.CreateFormFile("book", name)
	if e != nil {
		t.Fatal(e)
	}
	p.Write(data)
	m.Close()
	r := httptest.NewRequest("POST", "http://kobo/api/upload", &body)
	r.Header.Set("Content-Type", m.FormDataContentType())
	r.Header.Set("Cookie", "kobo_session=test-cookie")
	r.Header.Set("X-CSRF-Token", "test-csrf")
	r.Header.Set("X-Upload-ID", id)
	r.Header.Set("X-Upload-Name", url.QueryEscape(name))
	r.Header.Set("X-Upload-Size", metadataSize)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}
func TestRetryAfterLostAcknowledgement(t *testing.T) {
	s := fixture(t)
	id := strings.Repeat("a", 32)
	data := []byte("%PDF-1.7\nhello")
	for i := 0; i < 2; i++ {
		w := tracked(t, s, id, "retry.pdf", data, "14")
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body)
		}
	}
	entries, _ := os.ReadDir(s.dest)
	if len(entries) != 1 || s.count != 1 || len(s.received) != 1 {
		t.Fatal("retry duplicated file", len(entries), s.count)
	}
	w := call(s, "GET", "/api/upload-status?id="+id, "", true)
	var job uploadJob
	if json.Unmarshal(w.Body.Bytes(), &job) != nil || job.Status != "done" || job.Name != "retry.pdf" {
		t.Fatal(w.Code, w.Body)
	}
	w = tracked(t, s, id, "different.pdf", data, "14")
	if w.Code != 409 {
		t.Fatal("ID reused with different file", w.Code)
	}
	s.sessions["other-cookie"] = "another-owner"
	r := httptest.NewRequest("GET", "http://kobo/api/upload-status?id="+id, nil)
	r.Header.Set("Cookie", "kobo_session=other-cookie")
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if !strings.Contains(w.Body.String(), "unknown") {
		t.Fatal("another client read receipt")
	}
}
func TestFailedUploadCanRestartWholeFile(t *testing.T) {
	s := fixture(t)
	id := strings.Repeat("b", 32)
	if w := tracked(t, s, id, "broken.pdf", []byte("not a pdf"), "9"); w.Code != 400 {
		t.Fatal(w.Code)
	}
	w := call(s, "GET", "/api/upload-status?id="+id, "", true)
	if !strings.Contains(w.Body.String(), "unknown") {
		t.Fatal(w.Body)
	}
	w = tracked(t, s, id, "broken.pdf", []byte("%PDF-1.7\nhello"), "14")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
}
func TestInterruptedMultipartLeavesNoReceiptOrFile(t *testing.T) {
	s := fixture(t)
	id := strings.Repeat("c", 32)
	r := httptest.NewRequest("POST", "http://kobo/api/upload", io.MultiReader(strings.NewReader("--boundary\r\nContent-Disposition: form-data; name=\"book\"; filename=\"cut.pdf\"\r\n\r\n%PDF-1.7"), brokenReader{}))
	r.Header.Set("Content-Type", "multipart/form-data; boundary=boundary")
	r.Header.Set("Cookie", "kobo_session=test-cookie")
	r.Header.Set("X-CSRF-Token", "test-csrf")
	r.Header.Set("X-Upload-ID", id)
	r.Header.Set("X-Upload-Name", "cut.pdf")
	r.Header.Set("X-Upload-Size", "100")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 400 {
		t.Fatal(w.Code)
	}
	entries, _ := os.ReadDir(s.dest)
	if len(entries) != 0 {
		t.Fatal("partial file exposed")
	}
	if len(s.jobs) != 0 {
		t.Fatal("interrupted receipt leaked")
	}
}

type brokenReader struct{}

func TestQRAddressChoicePersistsForSession(t *testing.T) {
	s := fixture(t)
	s.url = "http://kobo-test.local:8080/"
	s.fallbackURL = "http://192.168.1.2:8080/"
	for _, choice := range []string{"ip", ""} {
		r := httptest.NewRequest("GET", "http://kobo/screen?address="+choice, nil)
		r.RemoteAddr = "127.0.0.1:1234"
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != 200 || !strings.Contains(w.Body.String(), `src="/qr.png?address=ip"`) {
			t.Fatal("IP choice lost", w.Code, w.Body)
		}
	}
	r := httptest.NewRequest("GET", "http://kobo/qr.png?address=ip", nil)
	r.RemoteAddr = "127.0.0.1:1234"
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	expected, _ := qrcode.Encode(s.fallbackURL, qrcode.Medium, 420)
	if !bytes.Equal(w.Body.Bytes(), expected) {
		t.Fatal("QR did not encode the fallback IP")
	}
	r = httptest.NewRequest("GET", "http://kobo/screen?address=local", nil)
	r.RemoteAddr = "127.0.0.1:1234"
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if !strings.Contains(w.Body.String(), `src="/qr.png"`) {
		t.Fatal("could not switch back to local name")
	}
}

func (brokenReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func TestDeviceNamePersistenceAndValidation(t *testing.T) {
	config := filepath.Join(t.TempDir(), "settings.json")
	a, e := loadDeviceName(config)
	if e != nil || !strings.HasPrefix(a, "kobo-") {
		t.Fatal(a, e)
	}
	b, e := loadDeviceName(config)
	if e != nil || a != b {
		t.Fatal("default changed")
	}
	for _, bad := range []string{"../escape", "-bad", "bad-", "a.b", "", strings.Repeat("x", 49)} {
		if _, e := normalizeName(bad); e == nil {
			t.Fatal("accepted", bad)
		}
	}
	name, e := normalizeName("Kobo-Jachym.local")
	if e != nil || name != "kobo-jachym" {
		t.Fatal(name, e)
	}
	if e := saveDeviceName(config, name); e != nil {
		t.Fatal(e)
	}
	b, e = loadDeviceName(config)
	if e != nil || b != name {
		t.Fatal(b, e)
	}
	s := fixture(t)
	s.settingsPath = config
	r := httptest.NewRequest("POST", "http://kobo/device-name", strings.NewReader("token=test-session&name=kobo-new"))
	r.RemoteAddr = "127.0.0.1:1234"
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	r = httptest.NewRequest("POST", "http://kobo/device-name", strings.NewReader("token=test-session&name=kobo-evil"))
	r.RemoteAddr = "192.168.1.3:1234"
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 404 {
		t.Fatal("remote rename allowed")
	}
}
