package main

import (
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
)

func localSharing(s *receiver, form url.Values) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "http://kobo/share-settings", strings.NewReader(form.Encode()))
	r.RemoteAddr = "127.0.0.1:1234"
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}
func TestSharingRestrictsEveryDownloadRoute(t *testing.T) {
	s := fixture(t)
	seed(t, s, "visible.pdf")
	seed(t, s, "hidden.pdf")
	s.shareAll = false
	s.shared = map[string]bool{"visible.pdf": true}
	w := call(s, "GET", "/api/files", "", true)
	if !strings.Contains(w.Body.String(), "visible.pdf") || strings.Contains(w.Body.String(), "hidden.pdf") {
		t.Fatal(w.Body)
	}
	if w := call(s, "GET", "/download?file=hidden.pdf", "", true); w.Code != 404 {
		t.Fatal("unshared direct download", w.Code)
	}
	for _, route := range []string{"/zip", "/review"} {
		if w := call(s, "POST", route, "csrf=test-csrf&file=hidden.pdf", true); w.Code != 400 {
			t.Fatal("unshared selection", route, w.Code)
		}
	}
	w = call(s, "GET", "/basic", "", true)
	if strings.Contains(w.Body.String(), "hidden.pdf") {
		t.Fatal("basic view leaked unshared file")
	}
	s.shared = map[string]bool{}
	w = call(s, "GET", "/api/files", "", true)
	if w.Body.String() != "[]\n" {
		t.Fatal("empty selection exposed books", w.Body)
	}
}
func TestRememberShareAllAndRenamePreservesChoice(t *testing.T) {
	s := fixture(t)
	s.settingsPath = filepath.Join(t.TempDir(), "settings.json")
	if e := saveSettings(s.settingsPath, deviceSettings{Name: "kobo-test"}); e != nil {
		t.Fatal(e)
	}
	form := url.Values{"token": {s.token}, "mode": {"all"}, "remember": {"1"}}
	if w := localSharing(s, form); w.Code != 303 {
		t.Fatal(w.Code, w.Body)
	}
	cfg, e := loadSettings(s.settingsPath)
	if e != nil || !cfg.ShareAll || !s.shareAll {
		t.Fatal(cfg, e)
	}
	if e := saveDeviceName(s.settingsPath, "kobo-renamed"); e != nil {
		t.Fatal(e)
	}
	cfg, e = loadSettings(s.settingsPath)
	if e != nil || !cfg.ShareAll {
		t.Fatal("rename forgot remembered sharing")
	}
	seed(t, s, "one.pdf")
	form = url.Values{"token": {s.token}, "mode": {"selected"}, "shown": {"one.pdf"}, "file": {"one.pdf"}}
	if w := localSharing(s, form); w.Code != 303 {
		t.Fatal(w.Code, w.Body)
	}
	cfg, e = loadSettings(s.settingsPath)
	if e != nil || cfg.ShareAll || s.shareAll || !s.shared["one.pdf"] {
		t.Fatal("selected mode not applied", cfg, e)
	}
	// Per-page edits keep selections on other pages, and remote clients cannot edit.
	seed(t, s, "two.pdf")
	form.Set("shown", "two.pdf")
	form.Set("file", "two.pdf")
	if w := localSharing(s, form); w.Code != 303 {
		t.Fatal(w.Code)
	}
	if !s.shared["one.pdf"] || !s.shared["two.pdf"] {
		t.Fatal("other page selection lost")
	}
	if w := call(s, "POST", "/share-settings", form.Encode(), true); w.Code != 404 {
		t.Fatal("remote sharing change accepted", w.Code)
	}
}
