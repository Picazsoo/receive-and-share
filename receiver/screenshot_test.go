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
	render(w, finishedPage, struct {
		Count int
		Total int64
		Files []receivedFile
	}{2, 2585000, []receivedFile{{Name: "Field notes.pdf", Type: "PDF", Size: 2400000, Time: "14:32"}, {Name: "Reading guide.pdf", Type: "PDF", Size: 185000, Time: "14:33"}}})
	if err := os.WriteFile(filepath.Join(dir, "finished.html"), w.Body.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}
}
