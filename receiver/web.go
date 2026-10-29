package main

import (
	"archive/zip"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	qrcode "github.com/skip2/go-qrcode"
	"io"
	"io/fs"
	"log"
	"math/big"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"
)

//go:embed web/*
var assets embed.FS

func secret() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
func (s *receiver) rotatePIN() {
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		panic(err)
	}
	s.pin = fmt.Sprintf("%06d", n.Int64())
	s.pinUntil = time.Now().Add(2 * time.Minute)
	s.misses = 0
}
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}
func (s *receiver) session(r *http.Request) string {
	c, e := r.Cookie("kobo_session")
	if e != nil {
		return ""
	}
	s.authMu.Lock()
	defer s.authMu.Unlock()
	return s.sessions[c.Value]
}
func equal(a, b string) bool { return a != "" && subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }
func foreign(r *http.Request) bool {
	o := r.Header.Get("Origin")
	return (o != "" && o != "null" && o != "http://"+r.Host) || r.Header.Get("Sec-Fetch-Site") == "cross-site"
}
func (s *receiver) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Reject DNS rebinding: only the printed address and Kobo loopback are served.
	s.netMu.RLock()
	hostOK := s.allowedHosts == nil || s.allowedHosts[r.Host]
	s.netMu.RUnlock()
	if !hostOK {
		log.Printf("Rejected Host: %q (local=%t, path=%s)", r.Host, local(r), r.URL.Path)
		http.Error(w, "Use the address printed on the Kobo", 403)
		return
	}
	rc := http.NewResponseController(w)
	if p := r.URL.Path; p == "/api/upload" || p == "/upload" || p == "/download" || p == "/zip" {
		r.Body = stallReader{r.Body, rc}
		w = stallWriter{w, rc}
		extendDeadlines(rc, transferStall)
	} else {
		extendDeadlines(rc, requestTimeout)
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "same-origin")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; connect-src 'self'; img-src 'self'; style-src 'self' 'unsafe-inline'; form-action 'self'; frame-ancestors 'none'")
	if r.URL.Path == "/finish" {
		s.finish(w, r)
		return
	}
	if r.URL.Path == "/local-heartbeat" {
		s.heartbeat(w, r)
		return
	}
	if r.URL.Path == "/device-name" {
		s.rename(w, r)
		return
	}
	if r.URL.Path == "/session-files" {
		s.sessionFiles(w, r)
		return
	}
	if r.URL.Path == "/share-settings" {
		s.shareSettings(w, r)
		return
	}
	if local(r) {
		switch r.URL.Path {
		case "/local.js":
			if r.Method == "GET" {
				s.browserScript(w)
				return
			}
		case "/health":
			if r.Method == "GET" {
				io.WriteString(w, "kobo-receive\n")
				return
			}
		case "/qr.png":
			if r.Method == "GET" {
				w.Header().Set("Content-Type", "image/png")
				s.netMu.RLock()
				qr := s.qr
				fallback := s.fallbackURL
				s.netMu.RUnlock()
				if r.URL.Query().Get("address") == "ip" && fallback != "" {
					if png, e := qrcode.Encode(fallback, qrcode.Medium, 420); e == nil {
						qr = png
					}
				}
				w.Write(qr)
				return
			}
		case "/screen":
			if r.Method == "GET" {
				s.source(w, r)
				return
			}
		case "/settings":
			if r.Method == "GET" {
				s.netMu.RLock()
				name := s.deviceName
				s.netMu.RUnlock()
				s.localRender(w, localPageHead+backToConnection+`<h1>Device name</h1><form action="/device-name" method="post"><input type="hidden" name="token" value="{{.Token}}"><label>Local network name<br><input name="name" value="{{display .Name}}" required maxlength="48">.local</label><p><small>Letters, numbers and hyphens. Changes apply the next time you start Receive & Share. A suffix is added if the name is already in use.</small></p><button>Save name</button></form></body></html>`, struct{ Token, Name string }{s.token, name})
				return
			}
		case "/renew":
			if r.Method != "POST" {
				http.Error(w, "POST required", 405)
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, 1024)
			if r.ParseForm() != nil || !equal(r.PostForm.Get("token"), s.token) {
				http.Error(w, "Open Receive & Share again", 403)
				return
			}
			s.authMu.Lock()
			s.rotatePIN()
			s.authMu.Unlock()
			http.Redirect(w, r, "/screen", 303)
			return
		}
	}
	if r.URL.Path == "/pair" {
		s.pair(w, r)
		return
	}
	if r.URL.Path == "/api/pair-challenge" && r.Method == "GET" {
		nonce := secret()
		http.SetCookie(w, &http.Cookie{Name: "kobo_pair", Value: nonce, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
		writeJSON(w, map[string]string{"nonce": nonce})
		return
	}
	csrf := s.session(r)
	if r.Method == "POST" && foreign(r) && csrf == "" {
		http.Error(w, "Cross-site request rejected", 403)
		return
	}
	if csrf == "" {
		if (r.URL.Path == "/" || r.URL.Path == "/basic") && r.Method == "GET" {
			s.pairForm(w, "")
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/download" || r.URL.Path == "/zip" {
			http.Error(w, "Session ended. Open the Kobo address and pair again.", 401)
			return
		}
		http.NotFound(w, r)
		return
	}
	if r.Method == "POST" {
		token := r.Header.Get("X-CSRF-Token")
		if r.URL.Path != "/api/upload" && r.URL.Path != "/upload" {
			r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
			if r.ParseForm() != nil {
				http.Error(w, "Invalid form", 400)
				return
			}
			token = r.PostForm.Get("csrf")
		}
		// The basic /upload form carries its token as a multipart field, checked in upload.
		if r.URL.Path != "/upload" && !equal(token, csrf) {
			http.Error(w, "Session changed. Reload this page before trying again.", 403)
			return
		}
	}
	switch {
	case r.Method == "GET" && r.URL.Path == "/":
		s.asset(w, r, "index.html")
	case r.Method == "GET" && (r.URL.Path == "/app.js" || r.URL.Path == "/styles.css"):
		s.asset(w, r, strings.TrimPrefix(r.URL.Path, "/"))
	case r.Method == "GET" && r.URL.Path == "/api/session":
		s.shareMu.RLock()
		revision := s.shareRevision
		all := s.shareAll
		s.shareMu.RUnlock()
		writeJSON(w, map[string]any{"csrf": csrf, "maxUpload": maxUpload, "instance": s.instance, "shareRevision": revision, "shareAll": all})
	case r.Method == "GET" && r.URL.Path == "/api/upload-status":
		s.uploadStatus(w, r)
	case r.Method == "GET" && r.URL.Path == "/api/files":
		files, err := s.books(r)
		if err != nil {
			http.Error(w, "Could not read book folders: "+err.Error(), 500)
			return
		}
		writeJSON(w, files)
	case r.Method == "GET" && r.URL.Path == "/basic":
		s.basic(w, r, csrf)
	case r.Method == "GET" && r.URL.Path == "/download":
		s.download(w, r)
	case r.Method == "POST" && r.URL.Path == "/review":
		s.review(w, r, csrf)
	case r.Method == "POST" && r.URL.Path == "/zip":
		s.zipBooks(w, r)
	case r.Method == "POST" && (r.URL.Path == "/api/upload" || r.URL.Path == "/upload"):
		s.retryableUpload(w, r)
	default:
		http.NotFound(w, r)
	}
}

const (
	requestTimeout = 5 * time.Minute
	// Large transfers on slow Wi-Fi may take longer than requestTimeout; they
	// only need to make progress within transferStall.
	transferStall = 2 * time.Minute
)

func extendDeadlines(rc *http.ResponseController, d time.Duration) {
	// Unsupported writers, such as test recorders, simply have no deadline.
	until := time.Now().Add(d)
	rc.SetReadDeadline(until)
	rc.SetWriteDeadline(until)
}

type stallReader struct {
	io.ReadCloser
	rc *http.ResponseController
}

func (r stallReader) Read(p []byte) (int, error) {
	extendDeadlines(r.rc, transferStall)
	return r.ReadCloser.Read(p)
}

type stallWriter struct {
	http.ResponseWriter
	rc *http.ResponseController
}

func (w stallWriter) Write(p []byte) (int, error) {
	extendDeadlines(w.rc, transferStall)
	return w.ResponseWriter.Write(p)
}
func (w stallWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (s *receiver) asset(w http.ResponseWriter, r *http.Request, name string) {
	b, err := assets.ReadFile("web/" + name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", mime.TypeByExtension(path.Ext(name)))
	w.Write(b)
}
func (s *receiver) source(w http.ResponseWriter, r *http.Request) {
	s.authMu.Lock()
	pin, until, misses := s.pin, s.pinUntil, s.misses
	s.authMu.Unlock()
	count, total, recent := s.receivedSnapshot(3)
	status := "Enter within 2 minutes. Pairing lasts for the receiving session."
	if time.Now().After(until) || misses >= 10 {
		pin = "Expired"
		status = "Tap New code to allow another device to pair."
	}
	s.netMu.Lock()
	if r.URL.Query().Get("address") == "ip" {
		s.preferIP = true
	} else if r.URL.Query().Get("address") == "local" {
		s.preferIP = false
	}
	address, fallback, discovery := s.url, s.fallbackURL, s.mdnsStatus
	canonical := address + "|" + fallback
	useIP := s.preferIP
	hasLocal := fallback != "" && address != fallback
	s.netMu.Unlock()
	qrSource := "/qr.png"
	fallbackLabel := "IP fallback"
	if useIP && hasLocal {
		address, fallback = fallback, address
		fallbackLabel = "Local name"
		qrSource = "/qr.png?address=ip"
	}
	s.localRender(w, sourcePage, struct {
		URL, Token, PIN, Status, Fallback, Discovery, Sharing, QRSource, FallbackLabel, Canonical string
		UseIP, HasLocal                                                                           bool
		Count                                                                                     int
		Total                                                                                     int64
		Recent                                                                                    []receivedFile
	}{address, s.token, pin, status, fallback, discovery, s.sharingSummary(), qrSource, fallbackLabel, canonical, useIP, hasLocal, count, total, recent})
}
func (s *receiver) pairForm(w http.ResponseWriter, message string, status ...int) {
	nonce := secret()
	http.SetCookie(w, &http.Cookie{Name: "kobo_pair", Value: nonce, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if len(status) > 0 {
		w.WriteHeader(status[0])
	}
	render(w, pairPage, struct{ Error, Nonce string }{message, nonce})
}
func (s *receiver) pair(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		http.Redirect(w, r, "/", 303)
		return
	}
	if r.Method != "POST" {
		http.Error(w, "POST required", 405)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	if r.ParseForm() != nil {
		http.Error(w, "Invalid form", 400)
		return
	}
	challenge, err := r.Cookie("kobo_pair")
	if err != nil || !equal(r.PostForm.Get("nonce"), challenge.Value) {
		http.Error(w, "Reload the pairing page and try again.", 403)
		return
	}
	s.authMu.Lock()
	defer s.authMu.Unlock()
	// Global attempt budget cannot be bypassed by changing the client's IP address.
	if time.Now().After(s.pinUntil) || s.misses >= 10 {
		s.pairForm(w, "Code expired or locked. Tap New code on the source Kobo.", 429)
		return
	}
	if time.Since(s.lastPair) < time.Second {
		w.Header().Set("Retry-After", "1")
		s.pairForm(w, "Wait one second before trying again.", 429)
		return
	}
	s.lastPair = time.Now()
	if !equal(r.PostForm.Get("code"), s.pin) {
		s.misses++
		s.pairForm(w, "Incorrect code. Check the source Kobo.", 403)
		return
	}
	if len(s.sessions) >= 16 {
		http.Error(w, "Too many paired devices. Restart Receive & Share on the source Kobo.", 429)
		return
	}
	id, csrf := secret(), secret()
	if s.sessions == nil {
		s.sessions = make(map[string]string)
	}
	s.sessions[id] = csrf
	http.SetCookie(w, &http.Cookie{Name: "kobo_session", Value: id, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
	if r.Header.Get("Accept") == "application/json" {
		writeJSON(w, map[string]string{"csrf": csrf, "instance": s.instance})
		return
	}
	target := "/"
	if r.PostForm.Get("basic") == "1" {
		target = "/basic"
	}
	http.Redirect(w, r, target, 303)
}

type book struct {
	Path     string    `json:"path"`
	Name     string    `json:"name"`
	Folder   string    `json:"folder"`
	Size     int64     `json:"size"`
	Type     string    `json:"type"`
	Modified time.Time `json:"modified"`
}

func allowed(p string) bool {
	if !fs.ValidPath(p) || strings.ContainsAny(p, "\\:") {
		return false
	}
	for _, v := range strings.Split(p, "/") {
		if strings.HasPrefix(v, ".") || strings.HasPrefix(v, "$") || strings.EqualFold(v, "System Volume Information") || strings.EqualFold(v, "LOST.DIR") {
			return false
		}
	}
	return true
}
func bookPath(p string) bool {
	e := strings.ToLower(path.Ext(p))
	return allowed(p) && (e == ".pdf" || e == ".epub")
}
func (s *receiver) openBook(p string) (*os.File, error) {
	if s.root == nil || !bookPath(p) {
		return nil, fmt.Errorf("not a book path")
	}
	parts := strings.Split(p, "/")
	for i := range parts {
		info, e := s.root.Lstat(strings.Join(parts[:i+1], "/"))
		if e != nil {
			return nil, e
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("symlinks are hidden")
		}
	}
	f, e := s.root.Open(p)
	if e != nil {
		return nil, e
	}
	info, e := f.Stat()
	if e != nil || !info.Mode().IsRegular() {
		f.Close()
		return nil, fmt.Errorf("not a regular file")
	}
	return f, nil
}
func (s *receiver) allBooks(r *http.Request) ([]book, error) {
	result := []book{}
	if s.root == nil {
		return result, fmt.Errorf("storage unavailable")
	}
	// No lock is needed: uploads are staged under hidden names and renamed into place.
	visited := 0
	err := fs.WalkDir(s.root.FS(), ".", func(p string, d fs.DirEntry, e error) error {
		if r.Context().Err() != nil {
			return r.Context().Err()
		}
		visited++
		if visited > 50000 {
			return fmt.Errorf("storage has too many entries")
		}
		if e != nil {
			return e
		}
		if p == "." {
			return nil
		}
		if !allowed(p) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if d.IsDir() || !bookPath(p) {
			return nil
		}
		info, e := d.Info()
		if e != nil {
			return e
		}
		// Empty files include names reserved by a save that is still renaming.
		if !info.Mode().IsRegular() || info.Size() == 0 {
			return nil
		}
		folder := path.Dir(p)
		if folder == "." {
			folder = ""
		}
		result = append(result, book{p, displayText(path.Base(p)), folder, info.Size(), strings.ToUpper(strings.TrimPrefix(path.Ext(p), ".")), info.ModTime()})
		return nil
	})
	sort.Slice(result, func(i, j int) bool { return strings.ToLower(result[i].Path) < strings.ToLower(result[j].Path) })
	return result, err
}
func (s *receiver) download(w http.ResponseWriter, r *http.Request) {
	if !s.canShare(r.URL.Query().Get("file")) {
		http.NotFound(w, r)
		return
	}
	if !s.beginDownload(w) {
		return
	}
	defer s.uploads.Done()
	s.mu.Lock()
	f, e := s.openBook(r.URL.Query().Get("file"))
	s.mu.Unlock()
	if e != nil {
		http.Error(w, "Book unavailable", 404)
		return
	}
	defer f.Close()
	info, e := f.Stat()
	if e != nil {
		http.Error(w, "Book unavailable", 404)
		return
	}
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": path.Base(r.URL.Query().Get("file"))}))
	w.Header().Set("Content-Type", "application/octet-stream")
	http.ServeContent(w, r, info.Name(), info.ModTime(), f)
	log.Print("Individual download served")
}
func (s *receiver) beginDownload(w http.ResponseWriter) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.finishing {
		http.Error(w, "Session is finishing", 503)
		return false
	}
	s.uploads.Add(1)
	return true
}

type selection struct {
	Files []book
	Total int64
	CSRF  string
}

func (s *receiver) selection(r *http.Request) (selection, []*os.File, error) {
	v := selection{}
	handles := []*os.File{}
	seen := map[string]bool{}
	fail := func(e error) (selection, []*os.File, error) {
		for _, f := range handles {
			f.Close()
		}
		return v, nil, e
	}
	values := r.PostForm["file"]
	if len(values) == 0 || len(values) > 100 {
		return fail(fmt.Errorf("select between 1 and 100 books"))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range values {
		if !s.canShare(p) {
			return fail(fmt.Errorf("book is not shared"))
		}
		if seen[p] {
			continue
		}
		seen[p] = true
		f, e := s.openBook(p)
		if e != nil {
			return fail(fmt.Errorf("book unavailable: %s", p))
		}
		handles = append(handles, f)
		info, e := f.Stat()
		if e != nil {
			return fail(e)
		}
		v.Total += info.Size()
		if v.Total > 512<<20 {
			return fail(fmt.Errorf("ZIP limit is 512 MiB; choose fewer books"))
		}
		v.Files = append(v.Files, book{Path: p, Name: path.Base(p), Size: info.Size(), Type: strings.ToUpper(strings.TrimPrefix(path.Ext(p), ".")), Modified: info.ModTime()})
	}
	return v, handles, nil
}
func (s *receiver) review(w http.ResponseWriter, r *http.Request, csrf string) {
	v, files, e := s.selection(r)
	if e != nil {
		http.Error(w, e.Error(), 400)
		return
	}
	for _, f := range files {
		f.Close()
	}
	v.CSRF = csrf
	render(w, reviewPage, v)
}
func (s *receiver) zipBooks(w http.ResponseWriter, r *http.Request) {
	if !s.beginDownload(w) {
		return
	}
	defer s.uploads.Done()
	if !s.transferMu.TryLock() {
		http.Error(w, "Another transfer is active. Retry shortly.", 409)
		return
	}
	defer s.transferMu.Unlock()
	v, files, e := s.selection(r)
	if e != nil {
		http.Error(w, e.Error(), 400)
		return
	}
	defer func() {
		for _, f := range files {
			f.Close()
		}
	}()
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="Receive & Share books.zip"`)
	z := zip.NewWriter(w)
	for i, b := range v.Files {
		if r.Context().Err() != nil {
			return
		}
		h := &zip.FileHeader{Name: b.Path, Method: zip.Store}
		h.SetModTime(b.Modified)
		out, e := z.CreateHeader(h)
		if e != nil {
			return
		}
		if _, e = io.CopyN(out, files[i], b.Size); e != nil {
			return
		}
	}
	if e := z.Close(); e != nil {
		log.Printf("ZIP transfer interrupted: %v", e)
		return
	}
	log.Printf("ZIP served: %d books, %d bytes", len(v.Files), v.Total)
}
func (s *receiver) basic(w http.ResponseWriter, r *http.Request, csrf string) {
	files, e := s.books(r)
	if e != nil {
		http.Error(w, "Cannot read folders", 500)
		return
	}
	folder := r.URL.Query().Get("folder")
	query := searchText(r.URL.Query().Get("q"))
	shown := []book{}
	folders := map[string]bool{}
	for _, b := range files {
		if b.Folder != "" {
			folders[b.Folder] = true
		}
		if (folder == "" || b.Folder == folder) && (query == "" || strings.Contains(searchText(b.Path), query)) {
			shown = append(shown, b)
		}
	}
	names := []string{}
	for f := range folders {
		names = append(names, f)
	}
	sort.Strings(names)
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	page = max(0, min(page, max(0, (len(shown)-1)/100)))
	total := len(shown)
	start := page * 100
	end := min(start+100, total)
	shown = shown[start:end]
	pageURL := func(n int) string { return "/basic?" + urlValues(folder, r.URL.Query().Get("q"), n) }
	previous, next := "", ""
	if page > 0 {
		previous = pageURL(page - 1)
	}
	if end < total {
		next = pageURL(page + 1)
	}
	render(w, basicPage, struct {
		Files               []book
		Folders             []string
		CSRF, Folder, Query string
		Previous, Next      string
		Total               int
	}{shown, names, csrf, folder, r.URL.Query().Get("q"), previous, next, total})
}
func urlValues(folder, query string, page int) string {
	return "folder=" + url.QueryEscape(folder) + "&q=" + url.QueryEscape(query) + "&page=" + strconv.Itoa(page)
}

const formControlsCSS = `.tool-button,.back-button{display:inline-block;box-sizing:border-box;min-height:44px;padding:10px 16px;border:2px solid black;background:white;color:black;font:18px/1.3 sans-serif;text-decoration:none;text-align:center;vertical-align:middle}.tool-button:active,.back-button:active{background:#ddd;color:black}.back-button{display:block;min-height:48px;text-align:left;margin:0 0 18px;font-weight:bold}.back-arrow{display:inline-block;position:relative;width:30px;height:24px;margin-right:12px;vertical-align:middle}.back-arrow:before{content:'';position:absolute;left:2px;top:4px;width:14px;height:14px;border-left:3px solid black;border-bottom:3px solid black;-webkit-transform:rotate(45deg);transform:rotate(45deg)}.back-arrow:after{content:'';position:absolute;left:1px;top:11px;width:28px;height:3px;background:black}.search-form button{margin-left:10px}.search-form input{margin-right:0}button,input[type=submit],input[type=button],input[type=reset]{-webkit-appearance:none;appearance:none;display:inline-block;box-sizing:border-box;min-height:44px;margin:8px 0;padding:10px 16px;border:2px solid black;border-radius:0;-webkit-border-radius:0;background:white;color:black;box-shadow:none;-webkit-box-shadow:none;font-family:sans-serif;font-size:18px;font-style:normal;font-weight:normal;line-height:1.3;text-align:center;text-decoration:none;vertical-align:middle;cursor:pointer}button:active,input[type=submit]:active,input[type=button]:active,input[type=reset]:active{background:black;color:white}button:disabled,input[type=submit]:disabled,input[type=button]:disabled,input[type=reset]:disabled{border-color:#777;background:#eee;color:#777;cursor:default}input:not([type]),input[type=text],input[type=search],input[type=number],input[type=password],textarea{-webkit-appearance:none;appearance:none;box-sizing:border-box;max-width:100%;min-height:44px;margin:4px 0;padding:10px 12px;border:2px solid black;border-radius:0;-webkit-border-radius:0;background:white;color:black;box-shadow:none;-webkit-box-shadow:none;font-family:sans-serif;font-size:18px;font-style:normal;font-weight:normal;line-height:1.3;vertical-align:middle}button:focus,input[type=submit]:focus,input[type=button]:focus,input[type=reset]:focus,input:not([type]):focus,input[type=text]:focus,input[type=search]:focus,input[type=number]:focus,input[type=password]:focus,textarea:focus{outline:2px dotted #555;outline-offset:3px}.finish:active{background:#333;color:white}`

const simpleHead = `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Receive & Share</title><style>body{font:18px/1.5 sans-serif;color:black;background:white;max-width:850px;margin:0 auto;padding:20px}a{color:black}button,input,select{font:inherit;max-width:100%;padding:8px}button{margin:8px 0}table{width:100%;border-collapse:collapse}td,th{padding:12px 4px;border-bottom:1px solid #aaa;text-align:left;vertical-align:top}small,.path{font-size:14px;word-wrap:break-word;overflow-wrap:anywhere}input[type=checkbox]{width:24px;height:24px}img{max-width:90%;height:auto}.error{border:2px solid black;padding:12px}.code{font-size:38px;letter-spacing:5px}h1{font-size:28px}form{margin:16px 0}.choice{position:relative;display:block;min-height:28px;line-height:28px;padding:7px 0 7px 42px;cursor:pointer}.choice input[type=checkbox],.choice input[type=radio]{position:absolute;left:0;top:8px;width:28px;height:28px;margin:0;padding:0;opacity:0;z-index:1;-webkit-appearance:none;appearance:none}.choice .control-mark{position:absolute;left:0;top:8px;display:block;width:24px;height:24px;box-sizing:content-box;border:2px solid black;background:white}.choice input[type=radio]+.control-mark{border-radius:50%;-webkit-border-radius:50%}.choice input[type=radio]:checked+.control-mark:after{content:'';position:absolute;left:5px;top:5px;width:14px;height:14px;background:black;border-radius:50%;-webkit-border-radius:50%}.choice input[type=checkbox]:checked+.control-mark{background:black}.choice input[type=checkbox]:checked+.control-mark:after{content:'';position:absolute;left:7px;top:2px;width:7px;height:14px;border:solid white;border-width:0 3px 3px 0;-webkit-transform:rotate(45deg);transform:rotate(45deg)}.choice input:focus+.control-mark{outline:2px dotted #777;outline-offset:3px}.choice input:disabled+.control-mark{border-color:#777}.choice input[type=checkbox]:disabled:checked+.control-mark{background:#777}` + formControlsCSS + `</style></head><body><h1 class="brand-heading">Receive & Share</h1>`
const localPageHead = simpleHead + `<style>.brand-heading{display:none}</style>`
const backToConnection = `<a class="back-button" href="/screen"><span class="back-arrow" aria-hidden="true"></span>Back to connection screen</a>`

const pairPage = simpleHead + `<h2>Pair with this Kobo</h2><p>Use the same Wi-Fi. Enter the six-digit code shown on the source Kobo.</p>{{if .Error}}<p class="error">{{.Error}}</p>{{end}}<form action="/pair" method="post"><input type="hidden" name="nonce" value="{{.Nonce}}"><label>Pairing code<br><input name="code" type="text" inputmode="numeric" pattern="[0-9]{6}" maxlength="6" autocomplete="one-time-code" required></label><p><label class="choice"><input type="checkbox" name="basic" value="1"><span class="control-mark" aria-hidden="true"></span> Basic / Kobo browser view</label></p><button type="submit">Pair</button></form><p><small>Pairing grants access to EPUB and PDF files for this receiving session. Transfers use your local Wi-Fi over HTTP.</small></p></body></html>`
const sourcePage = `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Receive & Share</title><style>
html,body{height:100%;margin:0;padding:0;background:white;color:black}body{display:table;width:100%;font:17px/1.35 sans-serif}.screen{display:table-cell;vertical-align:middle;padding:18px 24px;text-align:center;box-sizing:border-box}.panel{max-width:640px;margin:0 auto}h1{font-size:26px;line-height:1.2;margin:0 0 6px}.intro{font-size:16px;margin:0 0 10px}.qr-switch{display:table;border-collapse:collapse;table-layout:fixed;width:100%;max-width:420px;margin:8px auto 10px}.qr-switch a,.qr-switch span{display:table-cell;vertical-align:middle;width:50%;height:30px;padding:7px 12px;border:2px solid black;font:18px/1.3 sans-serif;text-decoration:none;color:black;background:white}.qr-switch .active{background:black;color:white;font-weight:bold}.qr-switch .unavailable{color:#777;background:#eee}.qr{display:block;width:200px;height:200px;max-width:80%;margin:0 auto 4px}.address{font-size:16px;margin:0 0 8px;word-wrap:break-word}.address small{font-size:13px}.pairing{margin:0}.pin-caption{font-size:15px;vertical-align:middle}.code{display:inline-block;font-size:38px;font-weight:bold;line-height:1.2;letter-spacing:4px;margin-left:12px;vertical-align:middle}.pairing .new-code{margin:0 0 0 16px;font-size:18px;padding:9px 14px}.status{font-size:13px;margin:5px 0 10px}.settings-area{border-top:1px solid #aaa;padding-top:10px;margin-top:10px}h2{font-size:18px;margin:0 0 8px}.settings-actions{display:table;width:100%;table-layout:fixed;border-spacing:8px 0}.settings-actions .tool-button{display:table-cell;width:50%;font-weight:bold}.sharing-summary{font-size:14px;margin:6px 0 10px}.session-area{border-top:1px solid #aaa;padding-top:10px;text-align:left}.session-heading h2{display:inline-block;margin:0 12px 0 0;vertical-align:middle}.session-heading .tool-button{font-size:16px;padding:9px 14px}.session-summary{font-size:14px;margin:7px 0}.session-file{border-top:1px solid #bbb;padding:7px 0;word-wrap:break-word}.session-file strong{font-size:16px}.session-type{display:inline-block;border:1px solid #777;padding:1px 4px;margin-right:7px;font-size:11px;vertical-align:middle}.session-meta{font-size:13px;margin-top:3px;color:#333}.session-empty{font-size:14px;margin:8px 0 10px}.all-received{display:block;margin:8px 0;text-align:center}.finish-form{margin:0}.finish{width:100%;min-height:50px;margin:10px 0 0;padding:12px 18px;background:black;color:white;font-size:19px;font-weight:bold}.help{font-size:13px;margin:9px 0 0}#browser-lease-status{font-size:12px!important;margin:4px 0 0}form{margin:0}
` + formControlsCSS + `</style></head><body><main class="screen"><div class="panel"><input type="hidden" id="source-address" value="{{.Canonical}}"><h1>Receive & Share</h1><p class="intro">Scan or enter the address on the other device.</p><div class="qr-switch" role="group" aria-label="QR address type">{{if .HasLocal}}{{if .UseIP}}<a href="/screen?address=local">Local name</a><span class="active" aria-current="true">IP address</span>{{else}}<span class="active" aria-current="true">Local name</span><a href="/screen?address=ip">IP address</a>{{end}}{{else}}<span class="unavailable">Local name</span><span class="active">IP address</span>{{end}}</div><img class="qr" src="{{.QRSource}}" width="200" height="200" alt="Connection address QR code"><p class="address">{{.URL}}{{if ne .URL .Fallback}}<br><small>{{.FallbackLabel}}: {{.Fallback}}</small>{{end}}</p><form class="pairing" action="/renew" method="post"><input type="hidden" name="token" value="{{.Token}}"><span class="pin-caption">Pairing code</span><strong class="code">{{.PIN}}</strong><button class="new-code">New code</button></form><p class="status">{{.Status}}</p><section class="settings-area"><h2>Settings</h2><div class="settings-actions"><a class="tool-button" href="/share-settings">What to share</a><a class="tool-button" href="/settings">Device name</a></div><p class="sharing-summary" id="source-sharing">Sharing: {{.Sharing}}</p></section><section class="session-area"><div class="session-heading"><h2>Received this session</h2><a class="tool-button" href="/screen">Refresh</a></div><p id="received-summary" class="session-summary">{{.Count}} file{{if ne .Count 1}}s{{end}} received · {{size .Total}}</p><div id="session-files" data-revision="{{.Count}}">{{range .Recent}}<div class="session-file"><span class="session-type">{{.Type}}</span><strong>{{display .Name}}</strong><div class="session-meta">Received · {{.SizeLabel}} · {{.Time}}</div></div>{{else}}<p class="session-empty">Waiting for files. This list updates automatically.</p>{{end}}</div><a id="all-received" class="tool-button all-received" href="/session-files" {{if le .Count 3}}hidden style="display:none"{{end}}>View all received files</a></section><form class="finish-form" action="/finish" method="post"><input type="hidden" name="token" value="{{.Token}}"><button class="finish">Finish &amp; refresh library</button></form><p class="help">Keep this Kobo awake · EPUB / PDF · 100 MiB per file · 20-minute session</p></div></main></body></html>`
const basicPage = simpleHead + `<p>Basic view · <a href="/">Enhanced view</a> · <a href="/basic">All books / refresh</a></p><h2>Send a book</h2><form action="/upload" method="post" enctype="multipart/form-data"><input type="hidden" name="csrf" value="{{.CSRF}}"><input name="book" type="file" accept=".epub,.pdf" required><button>Send to Kobo</button></form><p><small>100 MiB maximum. To transfer between Kobos, browse and download from the receiving Kobo.</small></p><h2>Files on Kobo</h2><form action="/basic" method="get"><label>Folder <select name="folder"><option value="">All book folders</option>{{range .Folders}}<option value="{{.}}" {{if eq . $.Folder}}selected{{end}}>{{.}}</option>{{end}}</select></label><br><label>Search <input name="q" value="{{display .Query}}"></label><button>Show files</button></form><form action="/review" method="post"><input type="hidden" name="csrf" value="{{.CSRF}}"><p>{{.Total}} books found. Showing {{len .Files}} on this page. Select up to 100 books (512 MiB) for a ZIP. Selection applies to this page.</p>{{if .Previous}}<a href="{{.Previous}}">Previous page</a>{{end}} {{if .Next}}<a href="{{.Next}}">Next page</a>{{end}}<button>Review selected books</button><table><tr><th>Select / file</th><th>Type / size</th><th>Download</th></tr>{{range .Files}}<tr><td><label class="choice"><input type="checkbox" name="file" value="{{.Path}}"><span class="control-mark" aria-hidden="true"></span> {{.Name}}</label><div class="path">/{{display .Path}}</div></td><td>{{.Type}}<br>{{size .Size}}</td><td><a href="/download?file={{urlquery .Path}}">Download</a></td></tr>{{else}}<tr><td>No books found.</td></tr>{{end}}</table><button>Review selected books</button></form><p>System and hidden folders are excluded. Tap Finish &amp; refresh library on the source Kobo when done.</p></body></html>`
const reviewPage = simpleHead + `<h2>{{len .Files}} books selected</h2><p>Total: {{size .Total}} · one ZIP · folder paths preserved.</p><form action="/zip" method="post"><input type="hidden" name="csrf" value="{{.CSRF}}"><table><tr><th>Filename / path</th><th>Type / size</th></tr>{{range .Files}}<tr><td>{{.Name}}<div class="path">/{{.Path}}</div><input type="hidden" name="file" value="{{.Path}}"></td><td>{{.Type}}<br>{{size .Size}}</td></tr>{{end}}</table><button>Download ZIP</button></form><a href="/basic">Back to files</a></body></html>`
