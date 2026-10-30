package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	if len(os.Args) == 4 && os.Args[1] == "__convert-epub" {
		if err := conversionWorker(os.Args[2], os.Args[3]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func conversionEPUB(t *testing.T, chapter string) []byte {
	t.Helper()
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	for _, f := range []struct{ name, body string }{
		{"mimetype", "application/epub+zip"},
		{"META-INF/container.xml", `<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container"><rootfiles><rootfile full-path="content.opf" media-type="application/oebps-package+xml"/></rootfiles></container>`},
		{"content.opf", `<package xmlns="http://www.idpf.org/2007/opf" version="2.0" unique-identifier="id"><metadata xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>Conversion test</dc:title><dc:identifier id="id">test</dc:identifier><dc:language>en</dc:language></metadata><manifest><item id="c" href="chapter.xhtml" media-type="application/xhtml+xml"/></manifest><spine><itemref idref="c"/></spine></package>`},
		{"chapter.xhtml", `<html xmlns="http://www.w3.org/1999/xhtml"><head><title>Test</title></head><body><p>` + chapter + `</p></body></html>`},
	} {
		w, err := z.Create(f.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = io.WriteString(w, f.body); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func postConversion(t *testing.T, s *receiver, name, id string, data []byte, convert, basic bool) *httptest.ResponseRecorder {
	t.Helper()
	var b bytes.Buffer
	m := multipart.NewWriter(&b)
	if basic {
		m.WriteField("csrf", "test-csrf")
		if convert {
			m.WriteField("convert", "1")
		}
	}
	w, err := m.CreateFormFile("book", name)
	if err != nil {
		t.Fatal(err)
	}
	w.Write(data)
	m.Close()
	p := "/api/upload"
	if basic {
		p = "/upload"
	}
	r := httptest.NewRequest("POST", "http://kobo"+p, &b)
	r.Header.Set("Content-Type", m.FormDataContentType())
	r.Header.Set("Cookie", "kobo_session=test-cookie")
	if !basic {
		r.Header.Set("X-CSRF-Token", "test-csrf")
		if convert {
			r.Header.Set("X-Convert-EPUB", "1")
		}
	}
	if id != "" {
		r.Header.Set("X-Upload-ID", id)
		r.Header.Set("X-Upload-Name", url.QueryEscape(name))
		r.Header.Set("X-Upload-Size", strconv.Itoa(len(data)))
	}
	result := httptest.NewRecorder()
	s.ServeHTTP(result, r)
	return result
}

func TestConversionUploadAndLostAcknowledgement(t *testing.T) {
	s := fixture(t)
	data := conversionEPUB(t, "A book for conversion. Another sentence.")
	id := strings.Repeat("d", 32)
	for range 2 {
		w := postConversion(t, s, "story.epub", id, data, true, false)
		if w.Code != 200 || !strings.Contains(w.Body.String(), "story.kepub.epub") {
			t.Fatal(w.Code, w.Body)
		}
	}
	if s.count != 1 {
		t.Fatal("converted retry created a duplicate")
	}
	z, err := zip.OpenReader(filepath.Join(s.dest, "story.kepub.epub"))
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	f, err := z.Open("chapter.xhtml")
	if err != nil {
		t.Fatal(err)
	}
	chapter, _ := io.ReadAll(f)
	f.Close()
	if !bytes.Contains(chapter, []byte("koboSpan")) {
		t.Fatal("not a converted KEPUB")
	}
	info, _ := os.Stat(filepath.Join(s.dest, "story.kepub.epub"))
	if s.received[0].Size != info.Size() {
		t.Fatal("receipt uses source size rather than converted size")
	}
	if _, err := os.Stat(filepath.Join(s.dest, "story.epub")); !os.IsNotExist(err) {
		t.Fatal("original retained")
	}
	if w := postConversion(t, s, "story.epub", id, data, false, false); w.Code != 409 {
		t.Fatal("conversion mode changed for existing receipt", w.Code)
	}
	if w := postConversion(t, s, "story.epub", strings.Repeat("a", 32), data, true, false); w.Code != 200 || !strings.Contains(w.Body.String(), "story (1).kepub.epub") {
		t.Fatal("duplicate lost the Kobo KEPUB suffix", w.Code, w.Body)
	}
}

func TestConversionBasicAndPassthrough(t *testing.T) {
	s := fixture(t)
	data := conversionEPUB(t, "A basic client book.")
	if w := postConversion(t, s, "basic.epub", "", data, true, true); w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	for _, c := range []struct {
		name    string
		data    []byte
		convert bool
	}{
		{"original.epub", data, false}, {"already.kepub.epub", data, true}, {"unchanged.pdf", []byte("%PDF-1.7\nuntouched"), true},
	} {
		if w := postConversion(t, s, c.name, "", c.data, c.convert, false); w.Code != 200 {
			t.Fatal(w.Code, w.Body)
		}
		got, err := os.ReadFile(filepath.Join(s.dest, c.name))
		if err != nil || !bytes.Equal(got, c.data) {
			t.Fatal("passthrough changed", c.name, err)
		}
	}
}

func TestConversionFailureReceiptAndOriginalFallback(t *testing.T) {
	s := fixture(t)
	data := conversionEPUB(t, strings.Repeat("x", 2<<20))
	id := strings.Repeat("e", 32)
	w := postConversion(t, s, "large.epub", id, data, true, false)
	if w.Code != 422 {
		t.Fatal(w.Code, w.Body)
	}
	w = call(s, "GET", "/api/upload-status?id="+id, "", true)
	var job uploadJob
	json.Unmarshal(w.Body.Bytes(), &job)
	if job.Status != "failed" || job.Message == "" || !job.Convert {
		t.Fatal(w.Body)
	}
	files, _ := os.ReadDir(s.dest)
	if len(files) != 0 {
		t.Fatal("failed conversion exposed a partial book")
	}
	w = postConversion(t, s, "large.epub", strings.Repeat("f", 32), data, false, false)
	if w.Code != 200 {
		t.Fatal("could not send original", w.Code, w.Body)
	}
	entries, _ := os.ReadDir(s.temp)
	if len(entries) != 0 {
		t.Fatal("temporary conversion files leaked")
	}
}

func TestConversionRejectsForgedZIPSize(t *testing.T) {
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	if _, err := z.CreateRaw(&zip.FileHeader{Name: "META-INF/container.xml", Method: zip.Store, UncompressedSize64: ^uint64(0)}); err != nil {
		t.Fatal(err)
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	input, output := filepath.Join(dir, "forged.epub"), filepath.Join(dir, "output")
	if err := os.WriteFile(input, b.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(output, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := conversionWorker(input, output); err == nil || !strings.Contains(err.Error(), "limits") {
		t.Fatal("forged expansion size not rejected before conversion", err)
	}
}
