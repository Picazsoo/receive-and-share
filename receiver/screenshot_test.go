package main

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// Opt-in documentation fixture; never compiled into the installed application.
func TestExportScreenshotFixture(t *testing.T) {
	dir := os.Getenv("SCREENSHOT_FIXTURE_DIR")
	if dir == "" {
		t.Skip("documentation export only")
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	render(w, finishedPage, finishedSession{Count: 2, Total: 2585000, Files: []receivedFile{{Name: "Field notes.pdf", Path: "Received Books/Field notes.pdf", Type: "PDF", Size: 2400000, Time: "14:32"}, {Name: "Reading guide.pdf", Path: "Received Books/Reading guide.pdf", Type: "PDF", Size: 185000, Time: "14:33"}}, DeletedCount: 1, DeletedTotal: 125000, Deleted: []receivedFile{{Name: "Old reading notes.pdf", Path: "Books/Old reading notes.pdf", Type: "PDF", Size: 125000, Time: "14:35"}}})
	if err := os.WriteFile(filepath.Join(dir, "finished.html"), w.Body.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}
}
