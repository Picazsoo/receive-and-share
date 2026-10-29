package main

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

func (s *receiver) canShare(p string) bool {
	s.shareMu.RLock()
	defer s.shareMu.RUnlock()
	return s.shareAll || s.shared[p]
}
func (s *receiver) books(r *http.Request) ([]book, error) {
	files, e := s.allBooks(r)
	if e != nil {
		return nil, e
	}
	result := []book{}
	s.shareMu.RLock()
	defer s.shareMu.RUnlock()
	for _, b := range files {
		if s.shareAll || s.shared[b.Path] {
			result = append(result, b)
		}
	}
	return result, nil
}
func (s *receiver) sharingSummary() string {
	s.shareMu.RLock()
	defer s.shareMu.RUnlock()
	if s.shareAll {
		if s.rememberShareAll {
			return "All books (remembered)"
		}
		return "All books (this session)"
	}
	if len(s.shared) == 1 {
		return "1 selected book"
	}
	return fmt.Sprintf("%d selected books", len(s.shared))
}
func (s *receiver) shareSettings(w http.ResponseWriter, r *http.Request) {
	if !local(r) {
		http.NotFound(w, r)
		return
	}
	if r.Method == "POST" {
		r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
		if r.ParseForm() != nil || !equal(r.PostForm.Get("token"), s.token) {
			http.Error(w, "Open Receive & Share again", 403)
			return
		}
		if r.PostForm.Get("mode") != "all" && r.PostForm.Get("mode") != "selected" {
			http.Error(w, "Choose a sharing mode", 400)
			return
		}
		shown := r.PostForm["shown"]
		files := r.PostForm["file"]
		if len(shown) > 100 || len(files) > 100 {
			http.Error(w, "Too many choices", 400)
			return
		}
		visible := map[string]bool{}
		for _, p := range shown {
			visible[p] = true
		}
		for _, p := range files {
			if !visible[p] {
				http.Error(w, "Selection changed. Reload this page.", 400)
				return
			}
			f, e := s.openBook(p)
			if e != nil {
				http.Error(w, "A selected book is unavailable. Reload the page.", 400)
				return
			}
			f.Close()
		}
		all := r.PostForm.Get("mode") == "all"
		remember := all && r.PostForm.Get("remember") == "1"
		s.settingsMu.Lock()
		cfg, e := loadSettings(s.settingsPath)
		if e == nil {
			cfg.ShareAll = remember
			e = saveSettings(s.settingsPath, cfg)
		}
		s.settingsMu.Unlock()
		if e != nil {
			http.Error(w, "Could not save sharing settings", 500)
			return
		}
		s.shareMu.Lock()
		if s.shared == nil {
			s.shared = map[string]bool{}
		}
		for _, p := range shown {
			delete(s.shared, p)
		}
		for _, p := range files {
			s.shared[p] = true
		}
		s.shareAll = all
		s.rememberShareAll = remember
		s.shareRevision++
		s.shareMu.Unlock()
		values := url.Values{"q": {r.PostForm.Get("q")}, "page": {r.PostForm.Get("page")}, "saved": {"1"}}
		http.Redirect(w, r, "/share-settings?"+values.Encode(), 303)
		return
	}
	if r.Method != "GET" {
		http.Error(w, "GET or POST required", 405)
		return
	}
	files, e := s.allBooks(r)
	if e != nil {
		http.Error(w, "Cannot read book folders", 500)
		return
	}
	query := searchText(r.URL.Query().Get("q"))
	filtered := []book{}
	for _, b := range files {
		if query == "" || strings.Contains(searchText(b.Path), query) {
			filtered = append(filtered, b)
		}
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	page = max(0, min(page, max(0, (len(filtered)-1)/100)))
	total := len(filtered)
	start := page * 100
	end := min(start+100, total)
	type row struct {
		book
		Checked bool
	}
	rows := []row{}
	s.shareMu.RLock()
	for _, b := range filtered[start:end] {
		rows = append(rows, row{b, s.shared[b.Path]})
	}
	all, remember := s.shareAll, s.rememberShareAll
	s.shareMu.RUnlock()
	link := func(n int) string {
		return "/share-settings?" + url.Values{"page": {strconv.Itoa(n)}, "q": {r.URL.Query().Get("q")}}.Encode()
	}
	previous, next := "", ""
	if page > 0 {
		previous = link(page - 1)
	}
	if end < total {
		next = link(page + 1)
	}
	s.localRender(w, sharingPage, struct {
		Files                                 []row
		Token, Query, Previous, Next, Summary string
		All, Remember, Saved                  bool
		Page, Total                           int
	}{rows, s.token, r.URL.Query().Get("q"), previous, next, s.sharingSummary(), all, remember, r.URL.Query().Get("saved") == "1", page, total})
}

const sharingPage = localPageHead + backToConnection + `<h1>Books to share</h1><p>Current: <b>{{.Summary}}</b></p>{{if .Saved}}<p class="error">Sharing choices saved.</p>{{end}}<form class="search-form" action="/share-settings" method="get"><label>Find books <input name="q" value="{{display .Query}}"></label><button>Search</button></form><form action="/share-settings" method="post"><input type="hidden" name="token" value="{{.Token}}"><input type="hidden" name="q" value="{{.Query}}"><input type="hidden" name="page" value="{{.Page}}"><p><label class="choice"><input type="radio" name="mode" value="all" {{if .All}}checked{{end}}><span class="control-mark" aria-hidden="true"></span> Share all books</label><label class="choice"><input type="radio" name="mode" value="selected" {{if not .All}}checked{{end}}><span class="control-mark" aria-hidden="true"></span> Share only selected books</label></p><p><label class="choice"><input type="checkbox" name="remember" value="1" {{if .Remember}}checked{{end}}><span class="control-mark" aria-hidden="true"></span> Remember “Share all” for next time</label></p><p><small>Selected books apply to this session only. With none selected, other devices can upload but cannot browse or download books. Changes affect new requests; downloads already underway may finish.</small></p><button>Save sharing choices</button><p>{{.Total}} matching books. Save before changing pages or searching.</p><table>{{range .Files}}<tr><td><input type="hidden" name="shown" value="{{.Path}}"><label class="choice"><input type="checkbox" name="file" value="{{.Path}}" {{if .Checked}}checked{{end}}><span class="control-mark" aria-hidden="true"></span> {{display .Name}}</label><div class="path">/{{display .Path}}</div></td><td>{{.Type}}<br>{{size .Size}}</td></tr>{{else}}<tr><td>No matching books.</td></tr>{{end}}</table><button>Save sharing choices</button></form>{{if .Previous}}<a href="{{.Previous}}">Previous page</a>{{end}} {{if .Next}}<a href="{{.Next}}">Next page</a>{{end}}` + backToConnection + `</body></html>`
