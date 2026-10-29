package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	pkg := filepath.Join(root, "package")
	elf := make([]byte, 20)
	copy(elf, []byte{127, 'E', 'L', 'F', 1, 1})
	elf[18] = 40
	inputs := map[string][]byte{
		".adds/kobo-receive/kobo-receive":            elf,
		".adds/kobo-receive/control.sh":              []byte("#!/bin/sh\r\necho ready\r\n"),
		".adds/kobo-receive/QR-LICENSE.txt":          []byte("license"),
		".adds/kobo-receive/licenses/dependency.txt": []byte("license"),
		".adds/nm/kobo-receive":                      []byte("menu_item:main:Receive & Share:cmd_spawn:quiet:launch"),
		"README.md":                                  []byte("readme"),
		".adds/kobo-receive/settings.json":           []byte("private settings"),
		".adds/kobo-receive/port.txt":                []byte("80"),
		".adds/nm/config":                            []byte("custom menu"),
	}
	for name, data := range inputs {
		p := filepath.Join(pkg, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "guide.md"), []byte("guide"), 0644); err != nil {
		t.Fatal(err)
	}
	return pkg
}

func TestArchivePreservesUserSettingsMenusAndDependencies(t *testing.T) {
	files, err := collect(fixture(t), "", "none")
	if err != nil {
		t.Fatal(err)
	}
	existing := map[string]string{
		app + "settings.json": "saved settings", app + "port.txt": "80",
		"mnt/onboard/.adds/nm/config":           "custom menu",
		"usr/local/Kobo/imageformats/libnm.so":  "newer NickelMenu",
		"usr/local/Kobo/imageformats/libndb.so": "newer NickelDBus",
		"mnt/onboard/Received Books/book.pdf":   "book contents",
	}
	output := filepath.Join(t.TempDir(), "KoboRoot.tgz")
	if err := writeArchive(output, files); err != nil {
		t.Fatal(err)
	}
	compressed, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	gz, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	seen := 0
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if _, conflict := existing[h.Name]; conflict {
			t.Fatal("overwrites existing user data/dependency", h.Name)
		}
		if h.Uid != 0 || h.Gid != 0 || h.Typeflag != tar.TypeReg {
			t.Fatal(h)
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			t.Fatal(err)
		}
		if h.Name == app+"control.sh" && (h.Mode != 0755 || strings.Contains(string(data), "\r")) {
			t.Fatal("invalid shell script mode or line endings")
		}
		seen++
	}
	if seen != len(files) {
		t.Fatal(seen, len(files))
	}
}

func TestDependencyBundlesAreExplicitAndVerified(t *testing.T) {
	inputs := os.Getenv("INSTALLER_INPUTS")
	if inputs == "" {
		t.Skip("official downloads not supplied")
	}
	for _, mode := range []string{"none", "nickeldbus", "all"} {
		files, err := collect(fixture(t), inputs, mode)
		if err != nil {
			t.Fatal(mode, err)
		}
		_, nm := files["usr/local/Kobo/imageformats/libnm.so"]
		_, ndb := files["usr/local/Kobo/imageformats/libndb.so"]
		if nm != (mode == "all") || ndb != (mode != "none") {
			t.Fatal(mode, nm, ndb)
		}
		if _, ok := files["mnt/onboard/.adds/nm/doc"]; ok {
			t.Fatal("replaces NickelMenu documentation/config")
		}
	}
	if _, err := checkedRead(filepath.Join(inputs, dbus.archive), strings.Repeat("0", 64)); err == nil {
		t.Fatal("accepted mismatched checksum")
	}
}

func TestArchiveRejectsTraversal(t *testing.T) {
	for _, name := range []string{"../escape", "/absolute", "a/../escape", "a\\escape"} {
		err := writeArchive(filepath.Join(t.TempDir(), "KoboRoot.tgz"), map[string]file{name: {[]byte("bad"), 0644}})
		if err == nil {
			t.Fatal("accepted", name)
		}
	}
}

func TestRejectsWrongApplicationArchitecture(t *testing.T) {
	pkg := fixture(t)
	if err := os.WriteFile(filepath.Join(pkg, ".adds/kobo-receive/kobo-receive"), []byte("MZ Windows executable"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := collect(pkg, "", "none"); err == nil {
		t.Fatal("accepted a non-ARM executable")
	}
}
