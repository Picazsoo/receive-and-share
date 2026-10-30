package main

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestComposedDisplayKeepsOriginalDownloadPath(t *testing.T) {
	s := fixture(t)
	original := "Harry Potter ohnivy\u0301 poha\u0301r.pdf"
	shown := "Harry Potter ohnivý pohár.pdf"
	data := seed(t, s, original)
	w := call(s, "GET", "/api/files", "", true)
	var files []book
	if e := json.Unmarshal(w.Body.Bytes(), &files); e != nil {
		t.Fatal(e)
	}
	if len(files) != 1 || files[0].Name != shown || files[0].Path != original {
		t.Fatal(files)
	}
	w = call(s, "GET", "/download?file="+url.QueryEscape(original), "", true)
	if w.Code != 200 || !bytes.Equal(w.Body.Bytes(), data) {
		t.Fatal("display normalization changed the download path")
	}
	w = call(s, "GET", "/basic?q="+url.QueryEscape("ohnivý"), "", true)
	if w.Code != 200 || !strings.Contains(w.Body.String(), shown) {
		t.Fatal("composed search could not find decomposed filename", w.Body)
	}
}
func TestSessionReceiptsAndLocalLiveFeed(t *testing.T) {
	s := fixture(t)
	if w := post(t, s, "poha\u0301r.pdf", []byte("%PDF-1.7\nhello")); w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	r := httptest.NewRequest("POST", "http://kobo/local-heartbeat", strings.NewReader("token="+s.token+"&format=json"))
	r.RemoteAddr = "127.0.0.1:1234"
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	var feed struct {
		Count, Revision int
		Files           []receivedFile
		State           string
	}
	if e := json.Unmarshal(w.Body.Bytes(), &feed); e != nil {
		t.Fatal(e, w.Body)
	}
	if feed.Count != 1 || feed.Revision != 1 || len(feed.Files) != 1 || feed.Files[0].Name != "pohár.pdf" || feed.Files[0].Size != 14 || feed.State != "manual" {
		t.Fatal(feed)
	}
	if w := call(s, "GET", "/session-files", "", true); w.Code != 404 {
		t.Fatal("remote client read source session history", w.Code)
	}
	r = httptest.NewRequest("GET", "http://kobo/session-files", nil)
	r.RemoteAddr = "127.0.0.1:1234"
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "pohár.pdf") || !strings.Contains(w.Body.String(), "Back to connection screen") {
		t.Fatal(w.Code, w.Body)
	}
}

func TestReadableSizeUnits(t *testing.T) {
	for _, tc := range []struct {
		bytes int64
		want  string
	}{{605, "0.6 KB"}, {12700, "12.7 KB"}, {899000, "899.0 KB"}, {900000, "0.9 MB"}, {1174051, "1.2 MB"}, {1000000, "1.0 MB"}} {
		if got := humanSize(tc.bytes); got != tc.want {
			t.Fatalf("%d: %s", tc.bytes, got)
		}
	}
}
func TestFinishedScreenIncludesSessionBooks(t *testing.T) {
	s := fixture(t)
	s.allowDelete = true
	seed(t, s, "Books/deleted.pdf")
	seed(t, s, "Books/not-deleted.pdf")
	token := reviewDeletion(t, s, "Books/deleted.pdf", "Books/not-deleted.pdf")
	// Replace one reviewed file with different content to force a per-file refusal.
	if err := os.WriteFile(filepath.Join(s.dest, "Books/not-deleted.pdf"), []byte("%PDF-1.7 changed"), 0644); err != nil {
		t.Fatal(err)
	}
	if w := call(s, "POST", "/api/delete", deleteConfirmation(s, token), true); w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	if w := post(t, s, "finished.pdf", []byte("%PDF-1.7\nhello")); w.Code != 200 {
		t.Fatal(w.Code)
	}
	s.onFinish = func() error { return nil }
	r := httptest.NewRequest("POST", "http://kobo/finish", strings.NewReader("token="+s.token))
	r.RemoteAddr = "127.0.0.1:1234"
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	for _, text := range []string{"Session finished", "1 book received", "finished.pdf", "0.0 KB", "1 book deleted", "Books/deleted.pdf"} {
		if !strings.Contains(w.Body.String(), text) {
			t.Fatal("finished receipt missing", text, w.Body)
		}
	}
	if strings.Contains(w.Body.String(), "not-deleted.pdf") {
		t.Fatal("failed deletion appeared in finished history", w.Body)
	}
}
