package main

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func reviewDeletion(t *testing.T, s *receiver, files ...string) string {
	t.Helper()
	f := url.Values{"csrf": {"test-csrf"}, "file": files}
	w := call(s, "POST", "/api/delete-review", f.Encode(), true)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	var result struct{ Confirmation string }
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || result.Confirmation == "" {
		t.Fatal(w.Body, err)
	}
	return result.Confirmation
}
func deleteConfirmation(s *receiver, token string) string {
	return url.Values{"csrf": {"test-csrf"}, "confirmation": {token}}.Encode()
}

func TestDeletionRequiresSeparateLocalPermission(t *testing.T) {
	s := fixture(t)
	seed(t, s, "book.pdf")
	form := url.Values{"csrf": {"test-csrf"}, "file": {"book.pdf"}}.Encode()
	if w := call(s, "POST", "/api/delete-review", form, true); w.Code != 403 {
		t.Fatal("Share all enabled deletion", w.Code)
	}
	if w := call(s, "POST", "/api/delete-review", form, false); w.Code != 401 {
		t.Fatal("unauthenticated review", w.Code)
	}
	s.settingsPath = filepath.Join(t.TempDir(), "settings.json")
	if err := saveSettings(s.settingsPath, deviceSettings{Name: "kobo-test"}); err != nil {
		t.Fatal(err)
	}
	local := url.Values{"token": {s.token}, "mode": {"all"}, "remember": {"1"}, "allow-delete": {"1"}}
	if w := localSharing(s, local); w.Code != 303 {
		t.Fatal(w.Code, w.Body)
	}
	if !s.deletionAllowed() {
		t.Fatal("permission not enabled")
	}
	cfg, err := loadSettings(s.settingsPath)
	if err != nil || !cfg.ShareAll {
		t.Fatal(cfg, err)
	}
	data, _ := os.ReadFile(s.settingsPath)
	if strings.Contains(string(data), "delete") {
		t.Fatal("deletion permission persisted")
	}
	if w := call(s, "POST", "/share-settings", local.Encode(), true); w.Code != 404 {
		t.Fatal("remote permission change", w.Code)
	}
	if w := call(s, "GET", "/api/session", "", true); !strings.Contains(w.Body.String(), `"allowDelete":true`) {
		t.Fatal(w.Body)
	}
	token := reviewDeletion(t, s, "book.pdf")
	if w := call(s, "POST", "/api/delete", "confirmation="+token, true); w.Code != 403 {
		t.Fatal("missing CSRF accepted", w.Code)
	}
	local.Del("allow-delete")
	if w := localSharing(s, local); w.Code != 303 {
		t.Fatal(w.Code)
	}
	if w := call(s, "POST", "/api/delete", deleteConfirmation(s, token), true); w.Code != 403 {
		t.Fatal("revoked permission accepted", w.Code)
	}
	if _, err := os.Stat(filepath.Join(s.dest, "book.pdf")); err != nil {
		t.Fatal("book removed despite refusal", err)
	}
}

func TestDeletionRestrictsScopeAndDangerousPaths(t *testing.T) {
	s := fixture(t)
	s.allowDelete = true
	s.shareAll = false
	s.shared = map[string]bool{"shared.pdf": true}
	for _, p := range []string{"shared.pdf", "unshared.pdf", ".kobo/private.pdf", "not-a-book.txt"} {
		seed(t, s, p)
	}
	for _, p := range []string{"unshared.pdf", ".kobo/private.pdf", "../outside.pdf", "not-a-book.txt"} {
		w := call(s, "POST", "/api/delete-review", url.Values{"csrf": {"test-csrf"}, "file": {p}}.Encode(), true)
		if w.Code == 200 {
			t.Fatal("unsafe deletion reviewed", p)
		}
	}
	s.shareAll = true
	for _, p := range []string{".kobo/private.pdf", "../outside.pdf", "not-a-book.txt"} {
		w := call(s, "POST", "/api/delete-review", url.Values{"csrf": {"test-csrf"}, "file": {p}}.Encode(), true)
		if w.Code == 200 {
			t.Fatal("Share all exposed unsafe deletion", p)
		}
	}
	s.shareAll = false
	token := reviewDeletion(t, s, "shared.pdf", "shared.pdf")
	if len(s.deleteJobs[token].Files) != 1 {
		t.Fatal("duplicate confirmation entries")
	}
	s.shared = map[string]bool{}
	s.shareRevision++
	if w := call(s, "POST", "/api/delete", deleteConfirmation(s, token), true); w.Code != 403 {
		t.Fatal("changed scope accepted", w.Code)
	}
}

func TestDeletionResultsStaleFilesAndLostResponse(t *testing.T) {
	s := fixture(t)
	s.allowDelete = true
	seed(t, s, "one.pdf")
	seed(t, s, "changed.pdf")
	token := reviewDeletion(t, s, "one.pdf", "changed.pdf")
	if _, err := os.Stat(filepath.Join(s.dest, "one.pdf")); err != nil {
		t.Fatal("review deleted a file")
	}
	if err := os.WriteFile(filepath.Join(s.dest, "changed.pdf"), []byte("%PDF-1.7 different file and size"), 0644); err != nil {
		t.Fatal(err)
	}
	w := call(s, "POST", "/api/delete", deleteConfirmation(s, token), true)
	var results []deleteResult
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &results) != nil || len(results) != 2 || !results[0].Deleted || results[1].Deleted {
		t.Fatal(w.Code, w.Body)
	}
	if _, err := os.Stat(filepath.Join(s.dest, "one.pdf")); !os.IsNotExist(err) {
		t.Fatal("book not deleted", err)
	}
	seed(t, s, "one.pdf") // A new book at the same path must survive acknowledgement retry.
	retry := call(s, "POST", "/api/delete", deleteConfirmation(s, token), true)
	if retry.Body.String() != w.Body.String() {
		t.Fatal("retry did not return the original result")
	}
	if len(s.deleted) != 1 || s.deleted[0].Path != "one.pdf" {
		t.Fatal("session deletion history includes a failed deletion or a retry", s.deleted)
	}
	if _, err := os.Stat(filepath.Join(s.dest, "one.pdf")); err != nil {
		t.Fatal("retry deleted replacement", err)
	}
	s.sessions["test-cookie"] = "different-client"
	if w := call(s, "POST", "/api/delete", strings.ReplaceAll(deleteConfirmation(s, token), "test-csrf", "different-client"), true); w.Code != 409 {
		t.Fatal("another client used the confirmation", w.Code)
	}
}

func TestDeletionExpiredAndBasicReview(t *testing.T) {
	s := fixture(t)
	s.allowDelete = true
	seed(t, s, "basic.pdf")
	token := reviewDeletion(t, s, "basic.pdf")
	s.deleteJobs[token].Expires = time.Now().Add(-time.Second)
	if w := call(s, "POST", "/api/delete", deleteConfirmation(s, token), true); w.Code != 409 {
		t.Fatal("expired confirmation accepted")
	}
	if w := call(s, "GET", "/basic", "", true); !strings.Contains(w.Body.String(), "Review deletion") {
		t.Fatal("basic deletion control missing")
	}
	w := call(s, "POST", "/review", "csrf=test-csrf&action=delete&file=basic.pdf", true)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Delete permanently") || !strings.Contains(w.Body.String(), "basic.pdf") {
		t.Fatal(w.Code, w.Body)
	}
}
