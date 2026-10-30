package main

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path"
	"strings"
	"time"

	"github.com/pgaskin/kepubify/v4/kepub"
)

const conversionTimeout = 90 * time.Second
const conversionLimit = 128 << 20

func convertible(name string) bool {
	name = strings.ToLower(name)
	return strings.HasSuffix(name, ".epub") && !strings.HasSuffix(name, ".kepub.epub")
}

// Conversion runs independently of the HTTP connection, so a lost acknowledgement
// can be recovered through the upload receipt instead of creating a second book.
func convertUpload(input, output string) error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), conversionTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, "__convert-epub", input, output)
	cmd.Env = append(os.Environ(), "GOMEMLIMIT=96MiB", "GOMAXPROCS=1")
	if data, err := cmd.CombinedOutput(); err != nil {
		log.Printf("EPUB conversion failed: %v: %.1000s", err, data)
		return fmt.Errorf("Conversion failed or exceeded the Kobo's limits. You can send the original EPUB instead.")
	}
	return nil
}

// The worker accepts only a ZIP-backed filesystem, never paths referenced by an
// EPUB on the Kobo's filesystem. Check both declared and actual expanded sizes.
func conversionWorker(input, output string) error {
	ctx, cancel := context.WithTimeout(context.Background(), conversionTimeout)
	defer cancel()
	z, err := zip.OpenReader(input)
	if err != nil {
		return err
	}
	defer z.Close()
	if len(z.File) > 4096 {
		return fmt.Errorf("too many EPUB entries")
	}
	var total uint64
	for _, entry := range z.File {
		name := strings.TrimSuffix(entry.Name, "/")
		if !fs.ValidPath(name) || strings.Contains(name, "\\") {
			return fmt.Errorf("invalid EPUB entry")
		}
		limit := uint64(8 << 20)
		switch strings.ToLower(path.Ext(name)) {
		case ".html", ".htm", ".xhtml", ".xml", ".opf", ".ncx":
			limit = 2 << 20
		}
		if entry.UncompressedSize64 > limit || entry.UncompressedSize64 > conversionLimit-total {
			return fmt.Errorf("EPUB expands beyond conversion limits")
		}
		total += entry.UncompressedSize64
		if entry.FileInfo().IsDir() {
			continue
		}
		r, err := entry.Open()
		if err != nil {
			return err
		}
		n, err := io.Copy(io.Discard, io.LimitReader(contextInput{ctx, r}, int64(limit)+1))
		r.Close()
		if err != nil || uint64(n) != entry.UncompressedSize64 {
			return fmt.Errorf("invalid or oversized EPUB entry")
		}
	}
	w, err := os.OpenFile(output, os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	err = kepub.NewConverter().Convert(ctx, &conversionOutput{w, conversionLimit}, &z.Reader)
	if err == nil {
		err = w.Sync()
	}
	closeErr := w.Close()
	if err == nil {
		err = closeErr
	}
	return err
}

type contextInput struct {
	context.Context
	io.Reader
}

func (r contextInput) Read(p []byte) (int, error) {
	if err := r.Err(); err != nil {
		return 0, err
	}
	return r.Reader.Read(p)
}

type conversionOutput struct {
	io.Writer
	remaining int64
}

func (w *conversionOutput) Write(p []byte) (int, error) {
	if int64(len(p)) > w.remaining {
		return 0, fmt.Errorf("converted EPUB is too large")
	}
	n, err := w.Writer.Write(p)
	w.remaining -= int64(n)
	return n, err
}
