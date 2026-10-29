package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const app = "mnt/onboard/.adds/kobo-receive/"

type file struct {
	data []byte
	mode int64
}
type dependency struct {
	archive, digest, license, licenseDigest string
	paths                                   map[string]string
}

var menu = dependency{"nickelmenu-v0.6.0.tgz", "322ff9aa863860e8f5f7e0b55cae561c54bf95983b9bce1d19819d1225d064af", "NickelMenu-LICENSE", "16ab8b4d63c2b06fd0b0ad647e461478906124c9041b44b4cc6da73dcf9a87cc", map[string]string{
	"usr/local/Kobo/imageformats/libnm.so": "usr/local/Kobo/imageformats/libnm.so",
	"mnt/onboard/.adds/nm/doc":             app + "docs/NickelMenu.txt",
}}
var dbus = dependency{"nickeldbus-0.2.0.tgz", "9fdb3d16d0f43c1ea6f2f1264b10fcde1d72a674e55f12955de812d476eb5dc5", "NickelDBus-LICENSE", "2fb918e05c5e380ad674e6badb8c486b70237fa0afb6baa13cb52ac8224e1409", map[string]string{
	"etc/dbus-1/system.d/com-github-shermp-nickeldbus.conf": "etc/dbus-1/system.d/com-github-shermp-nickeldbus.conf",
	"usr/bin/qndb":                          "usr/bin/qndb",
	"mnt/onboard/.adds/nickeldbus":          "mnt/onboard/.adds/nickeldbus",
	"usr/local/Kobo/imageformats/libndb.so": "usr/local/Kobo/imageformats/libndb.so",
}}

func checkedRead(name, digest string) ([]byte, error) {
	data, err := os.ReadFile(name)
	if err != nil {
		return nil, err
	}
	if fmt.Sprintf("%x", sha256.Sum256(data)) != digest {
		return nil, fmt.Errorf("checksum mismatch: %s", name)
	}
	return data, nil
}
func addDependency(files map[string]file, inputs string, dep dependency) error {
	data, err := checkedRead(filepath.Join(inputs, dep.archive), dep.digest)
	if err != nil {
		return err
	}
	license, err := checkedRead(filepath.Join(inputs, dep.license), dep.licenseDigest)
	if err != nil {
		return err
	}
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer gz.Close()
	archive := tar.NewReader(gz)
	seen := map[string]bool{}
	for {
		h, err := archive.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if h.Typeflag == tar.TypeDir {
			continue
		}
		name := strings.TrimPrefix(h.Name, "./")
		dest, ok := dep.paths[name]
		if !ok || seen[name] || h.Typeflag != tar.TypeReg || h.Size > 16<<20 {
			return fmt.Errorf("unexpected dependency member: %s", h.Name)
		}
		content, err := io.ReadAll(archive)
		if err != nil {
			return err
		}
		seen[name] = true
		mode := int64(0644)
		if strings.HasSuffix(dest, ".so") || dest == "usr/bin/qndb" {
			mode = 0755
		}
		files[dest] = file{content, mode}
	}
	if len(seen) != len(dep.paths) {
		return fmt.Errorf("dependency payload incomplete: %s", dep.archive)
	}
	files[app+"licenses/"+dep.license] = file{license, 0644}
	return nil
}

func collect(packageDir, inputs, dependencies string) (map[string]file, error) {
	if dependencies != "none" && dependencies != "nickeldbus" && dependencies != "all" {
		return nil, fmt.Errorf("dependencies must be none, nickeldbus or all")
	}
	files := map[string]file{}
	add := func(src, dst string, mode int64) error {
		info, err := os.Lstat(src)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("not a regular input: %s", src)
		}
		data, err := os.ReadFile(src)
		if err != nil {
			return err
		}
		if strings.HasSuffix(dst, ".sh") {
			data = bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
		}
		files[dst] = file{data, mode}
		return nil
	}
	for _, f := range []struct {
		src, dst string
		mode     int64
	}{
		{".adds/kobo-receive/kobo-receive", app + "kobo-receive", 0755},
		{".adds/kobo-receive/control.sh", app + "control.sh", 0755},
		{".adds/kobo-receive/QR-LICENSE.txt", app + "QR-LICENSE.txt", 0644},
		{".adds/kobo-receive/LICENSE", app + "LICENSE", 0644},
		{".adds/nm/kobo-receive", "mnt/onboard/.adds/nm/kobo-receive", 0644},
		{"README.md", app + "README.md", 0644},
	} {
		if err := add(filepath.Join(packageDir, filepath.FromSlash(f.src)), f.dst, f.mode); err != nil {
			return nil, err
		}
	}
	executable := files[app+"kobo-receive"].data
	if len(executable) < 20 || !bytes.Equal(executable[:4], []byte{127, 'E', 'L', 'F'}) || executable[4] != 1 || executable[5] != 1 || binary.LittleEndian.Uint16(executable[18:20]) != 40 {
		return nil, fmt.Errorf("receiver must be a little-endian ELF32 ARM executable")
	}
	for _, dir := range []struct{ src, dst string }{
		{filepath.Join(packageDir, ".adds/kobo-receive/licenses"), app + "licenses/"},
		{filepath.Join(packageDir, "..", "docs"), app + "docs/"},
	} {
		if err := filepath.WalkDir(dir.src, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(dir.src, p)
			if err != nil {
				return err
			}
			return add(p, dir.dst+filepath.ToSlash(rel), 0644)
		}); err != nil {
			return nil, err
		}
	}
	if dependencies == "all" {
		if err := addDependency(files, inputs, menu); err != nil {
			return nil, err
		}
	}
	if dependencies != "none" {
		if err := addDependency(files, inputs, dbus); err != nil {
			return nil, err
		}
	}
	note := "Receive & Share installer\n\nIncluded dependencies: " + dependencies + "\n\nCopy this KoboRoot.tgz into the Kobo's .kobo folder, safely eject and wait for restart.\nUse NickelMenu > Receive & Share.\nExisting app settings, received books and other NickelMenu configuration files are not included or replaced.\nDependency variants replace the named dependency binaries; use them only when those dependencies are absent.\nSupported target: Kobo Libra 2, firmware 4.38.23697 (4.x). Not for firmware 5.x.\n"
	files[app+"INSTALL-PACKAGE.txt"] = file{[]byte(note), 0644}
	return files, nil
}

func writeArchive(output string, files map[string]file) error {
	if err := os.MkdirAll(filepath.Dir(output), 0755); err != nil {
		return err
	}
	out, err := os.CreateTemp(filepath.Dir(output), ".koboroot-*")
	if err != nil {
		return err
	}
	defer os.Remove(out.Name())
	gz := gzip.NewWriter(out)
	tw := tar.NewWriter(gz)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if path.Clean(name) != name || strings.HasPrefix(name, "/") || strings.HasPrefix(name, "../") || strings.Contains(name, "\\") {
			out.Close()
			return fmt.Errorf("invalid archive path: %s", name)
		}
		f := files[name]
		if err = tw.WriteHeader(&tar.Header{Name: name, Mode: f.mode, Size: int64(len(f.data)), Typeflag: tar.TypeReg, ModTime: time.Unix(0, 0), Uid: 0, Gid: 0, Format: tar.FormatUSTAR}); err != nil {
			out.Close()
			return err
		}
		if _, err = tw.Write(f.data); err != nil {
			out.Close()
			return err
		}
	}
	for _, close := range []func() error{tw.Close, gz.Close, out.Sync, out.Close} {
		if err = close(); err != nil {
			out.Close()
			return err
		}
	}
	return os.Rename(out.Name(), output)
}

func main() {
	packageDir := flag.String("package", "../package", "built package directory")
	inputs := flag.String("inputs", "../dist/installer-inputs", "verified upstream downloads")
	output := flag.String("output", "../dist/KoboRoot.tgz", "output archive")
	dependencies := flag.String("dependencies", "none", "none, nickeldbus or all")
	flag.Parse()
	files, err := collect(*packageDir, *inputs, *dependencies)
	if err == nil {
		err = writeArchive(*output, files)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("Created %s (%d files, dependencies: %s)\n", *output, len(files), *dependencies)
}
