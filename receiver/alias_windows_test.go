package main

import (
	"net/url"
	"path/filepath"
	"syscall"
	"testing"
)

func TestHiddenDirectoryShortAlias(t *testing.T) {
	s := fixture(t)
	seed(t, s, ".private/secret.pdf")
	long, _ := syscall.UTF16PtrFromString(filepath.Join(s.dest, ".private"))
	buf := make([]uint16, 32768)
	n, err := syscall.GetShortPathName(long, &buf[0], uint32(len(buf)))
	if err != nil || n == 0 {
		t.Fatal(n, err)
	}
	alias := filepath.Base(syscall.UTF16ToString(buf)) + "/secret.pdf"
	if alias == ".private/secret.pdf" {
		t.Skip("filesystem has no short aliases")
	}
	if w := call(s, "GET", "/api/files", "", true); w.Body.String() != "[]\n" {
		t.Fatal("fixture was not hidden")
	}
	if w := call(s, "GET", "/download?file="+url.QueryEscape(".private/secret.pdf"), "", true); w.Code != 404 {
		t.Fatal("long path was not blocked")
	}
	download := call(s, "GET", "/download?file="+url.QueryEscape(alias), "", true)
	zip := call(s, "POST", "/zip", url.Values{"csrf": {"test-csrf"}, "file": {alias}}.Encode(), true)
	review := call(s, "POST", "/review", url.Values{"csrf": {"test-csrf"}, "file": {alias}}.Encode(), true)
	t.Logf("hidden .private/secret.pdf via %s: download=%d payload=%q zip=%d", alias, download.Code, download.Body.String(), zip.Code)
	if download.Code == 200 || zip.Code == 200 || review.Code == 200 {
		t.Fatal("hidden directory can be downloaded through its short name")
	}
}
