package main

import (
	"fmt"
	"net/http"
	"os"
	"path"
	"strings"
	"time"
)

type deleteCandidate struct {
	Book book
	Info os.FileInfo
}
type deleteResult struct {
	Path    string `json:"path"`
	Deleted bool   `json:"deleted"`
	Message string `json:"message"`
}
type deleteJob struct {
	Owner    string
	Expires  time.Time
	Revision int
	Files    []deleteCandidate
	Results  []deleteResult
}

func (s *receiver) deletionAllowed() bool {
	s.shareMu.RLock()
	defer s.shareMu.RUnlock()
	return s.allowDelete
}
func deletionResponse(w http.ResponseWriter, r *http.Request, results []deleteResult) {
	if r.URL.Path == "/api/delete" {
		writeJSON(w, results)
		return
	}
	render(w, simpleHead+`<h2>Deletion results</h2><table>{{range .}}<tr><td><div class="path">/{{display .Path}}</div></td><td>{{.Message}}</td></tr>{{end}}</table><p>Tap Finish &amp; refresh library on the source Kobo to update My Books.</p><a class="tool-button" href="/basic">Back to files</a></body></html>`, results)
}

// The review grants a short-lived, client-bound confirmation for exact files.
// It does not grant permission: the device's current settings are checked again
// at commit, and the request cannot substitute paths in the confirmed batch.
func (s *receiver) deleteReview(w http.ResponseWriter, r *http.Request) {
	values := r.PostForm["file"]
	if len(values) == 0 || len(values) > 100 {
		http.Error(w, "Select between 1 and 100 books", 400)
		return
	}
	s.deleteMu.Lock()
	defer s.deleteMu.Unlock()
	if s.deleteJobs == nil {
		s.deleteJobs = make(map[string]*deleteJob)
	}
	for token, job := range s.deleteJobs {
		if time.Now().After(job.Expires) {
			delete(s.deleteJobs, token)
		}
	}
	if len(s.deleteJobs) >= 64 {
		http.Error(w, "Too many deletion reviews. Wait a few minutes.", 429)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.shareMu.RLock()
	defer s.shareMu.RUnlock()
	if !s.allowDelete {
		http.Error(w, "Deletion is disabled on the source Kobo", 403)
		return
	}
	if s.finishing {
		http.Error(w, "Session is finishing", 503)
		return
	}
	job := &deleteJob{Owner: s.session(r), Expires: time.Now().Add(2 * time.Minute), Revision: s.shareRevision}
	files := []book{}
	seen := map[string]bool{}
	var total int64
	for _, p := range values {
		if seen[p] {
			continue
		}
		seen[p] = true
		if !s.shareAll && !s.shared[p] {
			http.Error(w, "A selected book is not shared", 403)
			return
		}
		f, err := s.openBook(p)
		if err != nil {
			http.Error(w, "A selected book is unavailable. Refresh the list.", 400)
			return
		}
		info, err := f.Stat()
		f.Close()
		if err != nil {
			http.Error(w, "Cannot read a selected book", 400)
			return
		}
		b := book{Path: p, Name: displayText(path.Base(p)), Size: info.Size(), Type: strings.ToUpper(strings.TrimPrefix(path.Ext(p), ".")), Modified: info.ModTime()}
		job.Files = append(job.Files, deleteCandidate{b, info})
		files = append(files, b)
		total += info.Size()
	}
	token := secret()
	s.deleteJobs[token] = job
	if r.URL.Path != "/api/delete-review" {
		render(w, simpleHead+`<h2>Delete {{len .Files}} books?</h2><p>Total: {{size .Total}}</p><p>This permanently deletes the files. Reading progress and annotations may be removed when the library is refreshed.</p><table>{{range .Files}}<tr><td>{{display .Name}}<div class="path">/{{display .Path}}</div></td><td>{{.Type}}<br>{{size .Size}}</td></tr>{{end}}</table><form action="/delete" method="post"><input type="hidden" name="csrf" value="{{.CSRF}}"><input type="hidden" name="confirmation" value="{{.Confirmation}}"><button>Delete permanently</button></form><a class="tool-button" href="/basic">Cancel</a></body></html>`, struct {
			Files              []book
			Total              int64
			CSRF, Confirmation string
		}{files, total, job.Owner, token})
		return
	}
	writeJSON(w, map[string]any{"confirmation": token, "files": files, "total": total})
}

func (s *receiver) deleteBooks(w http.ResponseWriter, r *http.Request) {
	s.deleteMu.Lock()
	defer s.deleteMu.Unlock()
	job := s.deleteJobs[r.PostForm.Get("confirmation")]
	if job == nil || !equal(job.Owner, s.session(r)) || time.Now().After(job.Expires) {
		http.Error(w, "Deletion confirmation expired. Review the books again.", 409)
		return
	}
	// Retain results so retrying a lost response cannot delete a newly uploaded
	// replacement at the same path, or repeat a partially successful batch.
	if job.Results != nil {
		deletionResponse(w, r, job.Results)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.shareMu.RLock()
	defer s.shareMu.RUnlock()
	if !s.allowDelete || job.Revision != s.shareRevision {
		http.Error(w, "Sharing or deletion permission changed. Review the books again.", 403)
		return
	}
	if s.finishing {
		http.Error(w, "Session is finishing", 503)
		return
	}
	job.Results = []deleteResult{}
	for _, candidate := range job.Files {
		p := candidate.Book.Path
		result := deleteResult{Path: p, Message: "Deleted"}
		f, err := s.openBook(p)
		if err == nil {
			info, statErr := f.Stat()
			f.Close()
			if statErr != nil || !os.SameFile(candidate.Info, info) || candidate.Info.Size() != info.Size() || !candidate.Info.ModTime().Equal(info.ModTime()) {
				err = fmt.Errorf("changed")
			}
		}
		if err != nil {
			result.Message = "Book changed or is unavailable. Refresh and review again."
		} else if err = s.root.Remove(p); err != nil {
			result.Message = "Could not delete this book. It may be busy or storage is unavailable."
		} else {
			result.Deleted = true
			b := candidate.Book
			s.deleted = append(s.deleted, receivedFile{Name: b.Name, Path: b.Path, Size: b.Size, SizeLabel: humanSize(b.Size), Type: b.Type, Time: time.Now().Format("15:04")})
		}
		job.Results = append(job.Results, result)
	}
	job.Expires = time.Now().Add(10 * time.Minute)
	deletionResponse(w, r, job.Results)
}
