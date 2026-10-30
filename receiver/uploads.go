package main

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
)

type uploadJob struct {
	Status   string `json:"status"`
	Name     string `json:"name"`
	Original string `json:"original"`
	Size     int64  `json:"size"`
	Convert  bool   `json:"convert"`
	Message  string `json:"message,omitempty"`
}

var validUploadID = regexp.MustCompile(`^[a-f0-9]{32}$`)

func (s *receiver) jobKey(r *http.Request) string {
	return s.session(r) + "/" + r.Header.Get("X-Upload-ID")
}
func (s *receiver) uploadStatus(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if !validUploadID.MatchString(id) {
		http.Error(w, "Invalid upload ID", 400)
		return
	}
	s.jobsMu.Lock()
	job, ok := s.jobs[s.session(r)+"/"+id]
	s.jobsMu.Unlock()
	if !ok {
		job.Status = "unknown"
	}
	writeJSON(w, job)
}
func (s *receiver) retryableUpload(w http.ResponseWriter, r *http.Request) {
	convert := r.Header.Get("X-Convert-EPUB") == "1"
	if value := r.Header.Get("X-Convert-EPUB"); value != "" && value != "0" && value != "1" {
		http.Error(w, "Invalid conversion option", 400)
		return
	}
	id := r.Header.Get("X-Upload-ID")
	if id != "" {
		if !validUploadID.MatchString(id) {
			http.Error(w, "Invalid upload ID", 400)
			return
		}
		original, e := url.QueryUnescape(r.Header.Get("X-Upload-Name"))
		size, err := strconv.ParseInt(r.Header.Get("X-Upload-Size"), 10, 64)
		if e != nil || err != nil || size < 1 || size > maxUpload || len(original) > 512 {
			http.Error(w, "Invalid file metadata", 400)
			return
		}
		key := s.jobKey(r)
		s.jobsMu.Lock()
		job, ok := s.jobs[key]
		s.jobsMu.Unlock()
		if ok {
			if job.Original != original || job.Size != size || job.Convert != convert {
				http.Error(w, "Upload ID belongs to a different file", 409)
				return
			}
			if job.Status == "done" {
				writeJSON(w, job)
				return
			}
			if job.Status == "failed" {
				http.Error(w, job.Message, 422)
				return
			}
			http.Error(w, "This upload is still being checked. Retry shortly.", 409)
			return
		}
	}
	if !s.transferMu.TryLock() {
		http.Error(w, "Another transfer is active. Retry shortly.", 409)
		return
	}
	defer s.transferMu.Unlock()
	if id != "" {
		key := s.jobKey(r)
		s.jobsMu.Lock()
		if s.jobs == nil {
			s.jobs = make(map[string]uploadJob)
		}
		// The first request may have completed between lookup and transfer lock.
		if existing, ok := s.jobs[key]; ok {
			s.jobsMu.Unlock()
			original, _ := url.QueryUnescape(r.Header.Get("X-Upload-Name"))
			expectedSize, _ := strconv.ParseInt(r.Header.Get("X-Upload-Size"), 10, 64)
			if existing.Original != original || existing.Size != expectedSize || existing.Convert != convert {
				http.Error(w, "Upload ID belongs to a different file", 409)
				return
			}
			if existing.Status == "failed" {
				http.Error(w, existing.Message, 422)
				return
			}
			if existing.Status == "done" {
				writeJSON(w, existing)
			} else {
				http.Error(w, "Upload is still being checked", 409)
			}
			return
		}
		if len(s.jobs) >= 2048 {
			s.jobsMu.Unlock()
			http.Error(w, "Upload tracking limit reached. Finish this session before starting another.", 429)
			return
		}
		original, _ := url.QueryUnescape(r.Header.Get("X-Upload-Name"))
		size, _ := strconv.ParseInt(r.Header.Get("X-Upload-Size"), 10, 64)
		s.jobs[key] = uploadJob{Status: "receiving", Original: original, Size: size, Convert: convert}
		s.jobsMu.Unlock()
		defer func() {
			s.jobsMu.Lock()
			if status := s.jobs[key].Status; status != "done" && status != "failed" {
				delete(s.jobs, key)
			}
			s.jobsMu.Unlock()
		}()
	}
	s.upload(w, r)
}

func (s *receiver) conversionStatus(r *http.Request, status, message string) {
	if r.Header.Get("X-Upload-ID") == "" {
		return
	}
	s.jobsMu.Lock()
	defer s.jobsMu.Unlock()
	key := s.jobKey(r)
	job := s.jobs[key]
	job.Status, job.Message = status, message
	s.jobs[key] = job
}
func (s *receiver) completeJob(r *http.Request, saved string) {
	if r.Header.Get("X-Upload-ID") == "" {
		return
	}
	key := s.jobKey(r)
	s.jobsMu.Lock()
	job := s.jobs[key]
	job.Status = "done"
	job.Name = saved
	s.jobs[key] = job
	s.jobsMu.Unlock()
}
func (s *receiver) verifyJob(r *http.Request, name string, size int64) error {
	if r.Header.Get("X-Upload-ID") == "" {
		return nil
	}
	key := s.jobKey(r)
	s.jobsMu.Lock()
	job := s.jobs[key]
	s.jobsMu.Unlock()
	expected, e := cleanName(job.Original)
	if e != nil || expected != name || job.Size != size {
		return fmt.Errorf("file does not match its upload metadata")
	}
	return nil
}
