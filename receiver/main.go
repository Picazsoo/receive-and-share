package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"html/template"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	qrcode "github.com/skip2/go-qrcode"
)

const maxUpload = 100 << 20

type receiver struct {
	received                                                    []receivedFile
	preferIP                                                    bool
	leaseMu                                                     sync.Mutex
	browserFirst, browserLast                                   time.Time
	browserArmed, autoFinish                                    bool
	settingsMu                                                  sync.Mutex
	shareMu                                                     sync.RWMutex
	shareAll, rememberShareAll                                  bool
	shared                                                      map[string]bool
	shareRevision                                               int
	netMu                                                       sync.RWMutex
	deviceName, settingsPath, fallbackURL, mdnsStatus, instance string
	jobsMu                                                      sync.Mutex
	jobs                                                        map[string]uploadJob
	allowedHosts                                                map[string]bool
	root                                                        *os.Root
	authMu                                                      sync.Mutex
	pin                                                         string
	pinUntil                                                    time.Time
	lastPair                                                    time.Time
	misses                                                      int
	sessions                                                    map[string]string
	transferMu                                                  sync.Mutex
	token, url, dest, temp                                      string
	qr                                                          []byte
	mu                                                          sync.Mutex
	uploads                                                     sync.WaitGroup
	count                                                       int
	finishing                                                   bool
	finishMu                                                    sync.Mutex
	finished                                                    bool
	onFinish                                                    func() error
}

func wifiIP() (string, error) {
	i, err := net.InterfaceByName("wlan0")
	if err != nil {
		return "", err
	}
	addrs, err := i.Addrs()
	if err != nil {
		return "", err
	}
	for _, a := range addrs {
		ip, _, err := net.ParseCIDR(a.String())
		if err == nil && ip.To4() != nil && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() {
			return ip.String(), nil
		}
	}
	return "", fmt.Errorf("connect the Kobo to Wi-Fi first, then try again")
}

func local(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	return err == nil && net.ParseIP(host).IsLoopback()
}

func (s *receiver) finish(w http.ResponseWriter, r *http.Request) {
	log.Printf("Finish button request: method=%s local=%t origin=%q", r.Method, local(r), r.Header.Get("Origin"))
	if !local(r) {
		http.NotFound(w, r)
		return
	}
	if r.Method != "POST" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	// Kobo's embedded browser may retain a different native-page Origin when it
	// submits a local form. Authorize this local-only POST using the unguessable
	// session token instead. Neither the screen nor its token is exposed remotely.
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	if r.ParseForm() != nil || !equal(r.PostForm.Get("token"), s.token) {
		http.Error(w, "This receiving session has changed. Open Receive & Share again.", 403)
		return
	}
	if err := s.finishTransfers(); err != nil {
		log.Printf("Library refresh unavailable: %v", err)
		render(w, pageHead+`<h1>Import helper unavailable</h1><p>The library refresh could not start. The session remains active. Go back and try Finish &amp; refresh library again.</p><p>{{.}}</p></body></html>`, err.Error())
		return
	}
	w.Header().Set("Connection", "close")
	count, total, files := s.receivedSnapshot(0)
	render(w, finishedPage, struct {
		Count int
		Total int64
		Files []receivedFile
	}{count, total, files})
}

// finishTransfers serializes finish attempts: a concurrent caller waits for the
// attempt in progress and reports success only once the library scan was requested.
func (s *receiver) finishTransfers() error {
	s.finishMu.Lock()
	defer s.finishMu.Unlock()
	if s.finished {
		return nil
	}
	s.mu.Lock()
	s.finishing = true
	s.mu.Unlock()
	log.Print("Waiting for active transfers before library refresh")
	s.uploads.Wait()
	var err error
	if s.onFinish == nil {
		err = fmt.Errorf("native library refresh is available only on the Kobo")
	} else {
		err = s.onFinish()
	}
	if err != nil {
		s.mu.Lock()
		s.finishing = false
		s.mu.Unlock()
	} else {
		s.finished = true
	}
	return err
}

func cleanName(name string) (string, error) {
	name = strings.ReplaceAll(name, "\\", "/")
	name = filepath.Base(name)
	name = strings.Map(func(c rune) rune {
		if c < 32 || c == 127 || strings.ContainsRune(`<>:"|?*`, c) || c >= 0x202a && c <= 0x202e || c >= 0x2066 && c <= 0x2069 {
			return '_'
		}
		return c
	}, name)
	name = strings.Trim(name, " .")
	if len(name) > 180 || name == "" {
		return "", fmt.Errorf("please use a shorter file name")
	}
	ext := strings.ToLower(filepath.Ext(name))
	if ext != ".epub" && ext != ".pdf" {
		return "", fmt.Errorf("choose an EPUB or PDF file")
	}
	return name, nil
}

func validate(path, name string) error {
	if strings.EqualFold(filepath.Ext(name), ".pdf") {
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		b := make([]byte, 1024)
		n, _ := f.Read(b)
		if !bytes.Contains(b[:n], []byte("%PDF-")) {
			return fmt.Errorf("this file does not appear to be a PDF")
		}
		return nil
	}
	z, err := zip.OpenReader(path)
	if err != nil {
		return fmt.Errorf("this file is not a valid EPUB archive")
	}
	defer z.Close()
	for _, f := range z.File {
		if f.Name == "mimetype" && f.UncompressedSize64 <= 64 {
			reader, err := f.Open()
			if err != nil {
				return err
			}
			b, err := io.ReadAll(io.LimitReader(reader, 65))
			reader.Close()
			if err == nil && strings.TrimSpace(string(b)) == "application/epub+zip" {
				return nil
			}
		}
	}
	return fmt.Errorf("this EPUB is missing its EPUB mimetype")
}

func (s *receiver) upload(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	finishing := s.finishing
	if !finishing {
		s.uploads.Add(1)
	}
	s.mu.Unlock()
	if finishing {
		http.Error(w, "Receiving has finished. Start a new session on the Kobo.", 503)
		return
	}
	defer s.uploads.Done()
	r.Body = http.MaxBytesReader(w, r.Body, maxUpload+(1<<20))
	reader, err := r.MultipartReader()
	if err != nil {
		http.Error(w, "Choose a file using the upload form.", 400)
		return
	}
	var tempPath, name string
	var uploadedSize int64
	// The basic form sends its CSRF token as a field before the file, keeping it out of the URL.
	formToken := r.URL.Path != "/upload"
	defer func() {
		if tempPath != "" {
			os.Remove(tempPath)
		}
	}()
	for {
		part, e := reader.NextPart()
		if e == io.EOF {
			break
		}
		if e != nil {
			http.Error(w, "Upload interrupted or too large. Limit: 100 MiB.", 400)
			return
		}
		if part.FormName() == "csrf" && part.FileName() == "" {
			b, _ := io.ReadAll(io.LimitReader(part, 128))
			part.Close()
			formToken = formToken || equal(string(b), s.session(r))
			continue
		}
		if part.FormName() != "book" || part.FileName() == "" {
			part.Close()
			continue
		}
		if !formToken {
			http.Error(w, "Session changed. Reload this page before trying again.", 403)
			return
		}
		if tempPath != "" {
			http.Error(w, "Upload one file at a time.", 400)
			return
		}
		name, err = cleanName(part.FileName())
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		f, e := os.CreateTemp(s.temp, "upload-*")
		if e != nil {
			http.Error(w, "Cannot create upload file. Check Kobo storage.", 500)
			return
		}
		tempPath = f.Name()
		n, e := io.Copy(f, io.LimitReader(part, maxUpload+1))
		uploadedSize = n
		if e == nil {
			e = f.Sync()
		}
		closeErr := f.Close()
		if e != nil || closeErr != nil || n > maxUpload || n == 0 {
			http.Error(w, "Upload failed or exceeded 100 MiB. Check storage and try again.", 400)
			return
		}
		part.Close()
	}
	if tempPath == "" {
		http.Error(w, "Select an EPUB or PDF first.", 400)
		return
	}
	if err = validate(tempPath, name); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if err = s.verifyJob(r, name, uploadedSize); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	saved, err := s.save(tempPath, name)
	if err != nil {
		http.Error(w, "Could not save the book. Check free storage and retry.", 500)
		return
	}
	s.mu.Lock()
	s.count++
	s.received = append(s.received, newReceivedFile(s, saved, uploadedSize))
	s.mu.Unlock()
	s.completeJob(r, saved)
	if r.URL.Path == "/api/upload" {
		writeJSON(w, map[string]string{"name": saved})
	} else {
		render(w, successPage, struct{ Name, Back string }{saved, "/basic"})
	}
}

func (s *receiver) save(path, name string) (string, error) {
	staged, err := s.stage(path)
	if err != nil {
		return "", err
	}
	defer os.Remove(staged)
	// O_EXCL reserves a name without replacing existing books, including on the
	// Kobo's FAT volume; the complete staged file then replaces the empty placeholder.
	s.mu.Lock()
	defer s.mu.Unlock()
	stem, ext := strings.TrimSuffix(name, filepath.Ext(name)), filepath.Ext(name)
	for n := 0; n < 10000; n++ {
		candidate := name
		if n > 0 {
			candidate = fmt.Sprintf("%s (%d)%s", stem, n, ext)
		}
		dest := filepath.Join(s.dest, candidate)
		f, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
		if os.IsExist(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		f.Close()
		if err = os.Rename(staged, dest); err != nil {
			os.Remove(dest)
			return "", err
		}
		return candidate, nil
	}
	return "", fmt.Errorf("too many duplicate names")
}

// stage moves an upload to a hidden file in the book directory, so listings never
// expose partial books. It copies only when the temporary directory is on another volume.
func (s *receiver) stage(path string) (string, error) {
	f, err := os.CreateTemp(s.dest, ".receiving-*")
	if err != nil {
		return "", err
	}
	staged := f.Name()
	f.Close()
	if err = os.Rename(path, staged); err == nil {
		return staged, nil
	}
	if f, err = os.OpenFile(staged, os.O_WRONLY|os.O_TRUNC, 0644); err != nil {
		os.Remove(staged)
		return "", err
	}
	src, err := os.Open(path)
	if err == nil {
		_, err = io.Copy(f, src)
		src.Close()
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(staged)
		return "", err
	}
	return staged, nil
}

// Pages are constant strings, so each is parsed once and reused.
var pageTemplates sync.Map

func render(w http.ResponseWriter, page string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	t, ok := pageTemplates.Load(page)
	if !ok {
		parsed, err := template.New("page").Funcs(template.FuncMap{"display": displayText, "size": humanSize}).Parse(page)
		if err != nil {
			log.Print(err)
			http.Error(w, "Page unavailable", 500)
			return
		}
		t, _ = pageTemplates.LoadOrStore(page, parsed)
	}
	if err := t.(*template.Template).Execute(w, data); err != nil {
		log.Print(err)
	}
}

const pageHead = `<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Receive & Share</title><style>body{font-family:sans-serif;background:white;color:black;max-width:600px;margin:0 auto;padding:28px;line-height:1.5}h1{font-size:30px}p{font-size:20px}button,a.button{display:block;padding:18px;background:black;color:white;border:0;font-size:21px;text-align:center;text-decoration:none;margin-top:24px}input{max-width:100%;font-size:18px}small{font-size:16px}img{max-width:85%;height:auto}a{color:black}.qr{text-align:center}.address{font-size:16px;word-wrap:break-word}` + formControlsCSS + `</style></head><body>`
const screenPage = pageHead + `<h1>Receive & Share</h1><p>Scan with your phone's camera.</p><div class="qr"><img src="/qr.png" width="420" height="420" alt="Upload link QR code"></div><form action="/finish" method="post"><input type="hidden" name="token" value="{{.Token}}"><button type="submit">Finish &amp; refresh library</button></form><p>Connect your phone to the same Wi-Fi as this Kobo.</p><p class="address">{{.URL}}</p><small>Keep the Kobo awake. This session lasts up to 20 minutes. EPUB / PDF, up to 100 MiB per file.</small></body></html>`
const uploadPage = pageHead + `<h1>Send a book to Kobo</h1><p>Choose an EPUB or PDF from Files on your phone.</p><form action="{{.Action}}" method="post" enctype="multipart/form-data"><input type="file" name="book" accept=".epub,.pdf,application/epub+zip,application/pdf" required><button type="submit">Send to Kobo</button></form><p><small>Up to 100 MiB. Keep this page open until confirmation appears. Files stay on your local network.</small></p></body></html>`
const successPage = pageHead + `<h1>Book received</h1><p><b>{{.Name}}</b></p><p>Saved on your Kobo. When done sending, tap <b>Finish &amp; refresh library</b> on the Kobo's QR page.</p><a class="button" href="{{.Back}}">Send another book</a></body></html>`
const finishedPage = pageHead + `<style>.finished-files{list-style:none;padding:0}.finished-files li{border-top:1px solid #aaa;padding:14px 0;word-wrap:break-word}.finished-files strong{font-size:20px}.finished-meta{font-size:16px;margin-top:5px}</style><h1>Session finished</h1><p>Sharing has stopped and the library refresh has been requested.</p><h2>{{.Count}} book{{if ne .Count 1}}s{{end}} received</h2><p>Total: {{size .Total}}</p><ul class="finished-files">{{range .Files}}<li><strong>{{display .Name}}</strong><div class="finished-meta">{{.Type}} · {{size .Size}} · {{.Time}}</div></li>{{else}}<li>No books received during this session.</li>{{end}}</ul><p>Close this window to return to your Kobo.</p></body></html>`

func nickelCommand(method string) error {
	log.Printf("NickelDBus command started: %s", method)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "/usr/bin/qndb", "-m", method).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w: %s", method, err, output)
	}
	log.Printf("NickelDBus command succeeded: %s", method)
	return nil
}

func requestLibraryScan(command func(string) error, finished chan<- struct{}) error {
	if err := command("pfmRescanBooks"); err != nil {
		return err
	}
	select {
	case finished <- struct{}{}:
	default:
	}
	return nil
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

// run returns errors instead of exiting, so deferred cleanup such as Wi-Fi
// restoration always runs.
func run() error {
	rootDir := flag.String("root", "/mnt/onboard", "storage root for book browsing")
	dest := flag.String("dest", "/mnt/onboard/Received Books", "book directory")
	temp := flag.String("temp", "/mnt/onboard/.adds/kobo-receive/tmp", "temporary directory")
	ip := flag.String("ip", "", "advertised IPv4 address (default wlan0)")
	port := flag.Int("port", 8080, "HTTP port")
	ready := flag.String("ready", "", "ready file")
	ttl := flag.Duration("ttl", 20*time.Minute, "session lifetime")
	autoFinish := flag.Bool("auto-finish", runtime.GOOS == "linux", "finish after the local browser heartbeat stops")
	config := flag.String("config", "", "device-name settings file (default beside temporary directory)")
	mdns := flag.Bool("mdns", true, "advertise a .local hostname on Wi-Fi")
	keepWiFi := flag.Bool("keep-wifi", runtime.GOOS == "linux", "retain Wi-Fi while receiving and restore its previous preference on exit")
	flag.Parse()
	var err error
	if *ip == "" {
		*ip, err = wifiIP()
		// The previous session restores Wi-Fi asynchronously on exit. Its driver
		// may briefly remove wlan0 while an immediate restart is already launching.
		for attempt := 0; err != nil && attempt < 20; attempt++ {
			time.Sleep(time.Second)
			*ip, err = wifiIP()
		}
		if err != nil {
			return err
		}
	}
	parsed := net.ParseIP(*ip)
	if parsed == nil || parsed.To4() == nil {
		return fmt.Errorf("an IPv4 address is required")
	}
	for _, d := range []string{*dest, *temp} {
		if err = os.MkdirAll(d, 0755); err != nil {
			return err
		}
	}
	token := make([]byte, 16)
	if _, err = rand.Read(token); err != nil {
		return err
	}
	s := &receiver{token: hex.EncodeToString(token), dest: *dest, temp: *temp}
	s.instance = secret()
	s.autoFinish = *autoFinish
	if *config == "" {
		*config = filepath.Join(filepath.Dir(*temp), "settings.json")
	}
	s.settingsPath = *config
	settings, err := loadSettings(*config)
	if err != nil {
		return err
	}
	s.deviceName = settings.Name
	s.shareAll = settings.ShareAll
	s.rememberShareAll = settings.ShareAll
	s.shared = make(map[string]bool)
	s.allowedHosts = map[string]bool{fmt.Sprintf("%s:%d", *ip, *port): true, fmt.Sprintf("127.0.0.1:%d", *port): true, *ip: true, "127.0.0.1": true}
	s.root, err = os.OpenRoot(*rootDir)
	if err != nil {
		return err
	}
	defer s.root.Close()
	s.sessions = make(map[string]string)
	s.rotatePIN()
	finishRequested := make(chan struct{}, 1)
	s.onFinish = func() error {
		return requestLibraryScan(nickelCommand, finishRequested)
	}
	s.url = fmt.Sprintf("http://%s:%d/", *ip, *port)
	s.fallbackURL = s.url
	s.qr, err = qrcode.Encode(s.url, qrcode.Medium, 420)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp4", fmt.Sprintf(":%d", *port))
	if err != nil {
		return err
	}
	server := &http.Server{Handler: s, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10}
	// Request timeouts are per request in ServeHTTP: transfers only need to keep making progress.
	var wifiSettled <-chan struct{}
	if *keepWiFi {
		releaseWiFi, settled := s.holdWiFi()
		defer releaseWiFi()
		wifiSettled = settled
	}
	networkCtx, stopNetwork := context.WithCancel(context.Background())
	var networkDone chan struct{}
	defer func() {
		stopNetwork()
		if networkDone != nil {
			select {
			case <-networkDone:
			case <-time.After(4 * time.Second):
				log.Print("mDNS shutdown timed out")
			}
		}
	}()
	if s.autoFinish {
		go s.watchBrowser(networkCtx)
	}
	if *mdns && !parsed.IsLoopback() {
		advertised := make(chan struct{})
		networkDone = make(chan struct{})
		go func() { defer close(networkDone); s.advertise(networkCtx, *port, advertised) }()
		select {
		case <-advertised:
		case <-time.After(3 * time.Second):
		}
	} else {
		s.mdnsStatus = "Use the numeric address on this network."
	}
	if wifiSettled != nil {
		// The launcher opens the browser once Wi-Fi looks stable; signal readiness
		// only after the ForceWifiOn reconnect so that check sees the final link.
		select {
		case <-wifiSettled:
		case <-time.After(20 * time.Second):
			log.Print("Wi-Fi reconnect still running; continuing startup")
		}
	}
	if *ready != "" {
		if err = os.WriteFile(*ready, []byte("ready\n"), 0600); err != nil {
			return err
		}
		defer os.Remove(*ready)
	}
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(stop)
	expiry := time.NewTimer(*ttl)
	defer expiry.Stop()
	serveErrors := make(chan error, 1)
	log.Printf("Receiver started (reconnect-mdns-1); session lifetime %s", *ttl)
	go func() { serveErrors <- server.Serve(listener) }()
	select {
	case <-finishRequested:
		log.Print("Library scan requested successfully; stopping HTTP server")
	case <-stop:
	case <-expiry.C:
	case err = <-serveErrors:
		if err != nil && err != http.ErrServerClosed {
			log.Print(err)
		}
	}
	// Stop accepting uploads, then let in-flight file writes finish before exit/import.
	if err = server.Shutdown(context.Background()); err != nil {
		log.Print(err)
	}
	return nil
}
