package main

import (
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/text/unicode/norm"
)

func displayText(s string) string { return norm.NFC.String(s) }
func searchText(s string) string  { return strings.ToLower(displayText(s)) }
func humanSize(n int64) string {
	if n >= 900000 {
		return fmt.Sprintf("%.1f MB", float64(n)/1000000)
	}
	return fmt.Sprintf("%.1f KB", float64(n)/1000)
}

type receivedFile struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	Size      int64  `json:"size"`
	SizeLabel string `json:"sizeLabel"`
	Type      string `json:"type"`
	Time      string `json:"time"`
}

func newReceivedFile(s *receiver, name string, size int64) receivedFile {
	p := filepath.Join(s.dest, name)
	if s.root != nil {
		if rel, e := filepath.Rel(s.root.Name(), p); e == nil {
			p = rel
		}
	}
	return receivedFile{displayText(name), filepath.ToSlash(p), size, humanSize(size), strings.ToUpper(strings.TrimPrefix(filepath.Ext(name), ".")), time.Now().Format("15:04")}
}
func (s *receiver) receivedSnapshot(limit int) (int, int64, []receivedFile) {
	s.mu.Lock()
	defer s.mu.Unlock()
	files := []receivedFile{}
	var bytes int64
	for _, f := range s.received {
		bytes += f.Size
	}
	start := 0
	if limit > 0 && len(s.received) > limit {
		start = len(s.received) - limit
	}
	for i := len(s.received) - 1; i >= start; i-- {
		files = append(files, s.received[i])
	}
	return s.count, bytes, files
}
func (s *receiver) sessionFiles(w http.ResponseWriter, r *http.Request) {
	if !local(r) {
		http.NotFound(w, r)
		return
	}
	if r.Method != "GET" {
		http.Error(w, "GET required", 405)
		return
	}
	count, total, files := s.receivedSnapshot(0)
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	page = max(0, min(page, max(0, (len(files)-1)/100)))
	start := page * 100
	end := min(start+100, len(files))
	previous, next := "", ""
	if page > 0 {
		previous = "/session-files?page=" + strconv.Itoa(page-1)
	}
	if end < len(files) {
		next = "/session-files?page=" + strconv.Itoa(page+1)
	}
	s.localRender(w, sessionFilesPage, struct {
		Files          []receivedFile
		Count          int
		Total          int64
		Previous, Next string
	}{files[start:end], count, total, previous, next})
}

const sessionFilesPage = localPageHead + backToConnection + `<h1>Received this session</h1><p><b>{{.Count}} file{{if ne .Count 1}}s{{end}} received</b> · {{size .Total}}</p><p>These files were saved successfully. Finish &amp; refresh library adds them to My Books.</p><table>{{range .Files}}<tr><td><strong>{{display .Name}}</strong><div class="path">/{{display .Path}}</div></td><td>{{.Type}}<br>{{size .Size}}<br>{{.Time}}</td></tr>{{else}}<tr><td>No files received yet.</td></tr>{{end}}</table>{{if .Previous}}<a class="tool-button" href="{{.Previous}}">Previous page</a>{{end}} {{if .Next}}<a class="tool-button" href="{{.Next}}">Next page</a>{{end}}` + backToConnection + `</body></html>`
