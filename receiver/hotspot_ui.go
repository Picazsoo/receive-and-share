package main

import (
	"context"
	"fmt"
	"html"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

func hotspotPeer(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	_, subnet, _ := net.ParseCIDR("192.168.42.0/24")
	return ip != nil && subnet.Contains(ip)
}

// The random WPA2 password is the hotspot's access boundary. Keep browser
// cookies, per-session CSRF tokens, Host checks and origin checks inside it.
func (s *receiver) hotspotSession(w http.ResponseWriter, r *http.Request) string {
	if s.hotspot == nil || !hotspotPeer(r) || r.Method != "GET" || foreign(r) {
		return ""
	}
	s.authMu.Lock()
	defer s.authMu.Unlock()
	if len(s.sessions) >= 16 {
		return ""
	}
	id, csrf := secret(), secret()
	if s.sessions == nil {
		s.sessions = map[string]string{}
	}
	s.sessions[id] = csrf
	http.SetCookie(w, &http.Cookie{Name: "kobo_session", Value: id, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
	return csrf
}

func (s *receiver) openHotspotUI(w http.ResponseWriter, r *http.Request) {
	if !local(r) || s.hotspot == nil {
		http.NotFound(w, r)
		return
	}
	if r.Method != "POST" {
		http.Error(w, "POST required", 405)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	if r.ParseForm() != nil || !equal(r.PostForm.Get("token"), s.token) {
		http.Error(w, "Session changed", 403)
		return
	}
	s.hotspotUIMu.Lock()
	defer s.hotspotUIMu.Unlock()
	if s.hotspotUIDone != nil {
		select {
		case <-s.hotspotUIDone:
			s.hotspotUIDone = nil
		default:
		}
	}
	if s.hotspotUIDone == nil {
		if s.hotspotContext == nil {
			http.Error(w, "Hotspot UI unavailable", 503)
			return
		}
		s.hotspotUIDone = make(chan struct{})
		if err := os.WriteFile(hotspotDir+"/ui-active", []byte("active\n"), 0600); err != nil {
			s.hotspotUIDone = nil
			http.Error(w, "Could not open hotspot screen", 500)
			return
		}
		done := s.hotspotUIDone
		go func() { defer close(done); s.hotspotDialogs(s.hotspotContext) }()
	}
	w.Write([]byte("ok\n"))
}

func hotspotDialog(ctx context.Context, title, body, accept string) (bool, error) {
	// qndb attaches the signal listener before showing the dialog. With no -t,
	// it waits until a button or X is pressed; receiver shutdown cancels it.
	args := []string{"-s", "dlgConfirmResult", "-m", "dlgConfirmAcceptReject", title, body, accept, "Finish & refresh"}
	cmd := exec.CommandContext(ctx, "/usr/bin/qndb", args...)
	hotspotChildOptions(cmd)
	output, e := cmd.CombinedOutput()
	if e != nil {
		return false, fmt.Errorf("hotspot dialog: %w: %s", e, output)
	}
	if strings.TrimSpace(string(output)) == "dlgConfirmResult 1" {
		return true, nil
	}
	if strings.TrimSpace(string(output)) == "dlgConfirmResult 0" {
		return false, nil
	}
	return false, fmt.Errorf("unexpected hotspot dialog response")
}

func (s *receiver) hotspotDialogs(ctx context.Context) {
	for name, data := range map[string][]byte{"wifi.png": s.hotspot.QR, "page.png": s.qr} {
		if e := os.WriteFile(hotspotDir+"/"+name, data, 0600); e != nil {
			log.Print(e)
			return
		}
	}
	wifi := true
	for ctx.Err() == nil {
		title := "1. Join Kobo Wi-Fi"
		body := `<p align="center"><img src="` + hotspotDir + `/wifi.png" width="420" height="420"></p><p>Network: <b>` + html.EscapeString(s.hotspot.SSID) + `</b><br>Password: <b>` + html.EscapeString(s.hotspot.Password) + `</b></p><p>No internet. Choose to stay connected.</p>`
		accept := "Next"
		if !wifi {
			title = "2. Open Receive & Share"
			body = `<p align="center"><img src="` + hotspotDir + `/page.png" width="420" height="420"></p><p>` + html.EscapeString(strings.TrimSuffix(s.url, "/")) + `</p><p>All books are shared. No pairing code is needed.<br>Deletion is disabled.</p>`
			accept = "Wi-Fi QR"
		}
		next, e := hotspotDialog(ctx, title, body, accept)
		if ctx.Err() != nil {
			return
		}
		if e != nil {
			log.Print(e)
			return
		}
		if next {
			wifi = !wifi
			continue
		}
		if e = s.finishTransfers(); e != nil {
			log.Printf("Hotspot finish: %v", e)
			_ = hotspotNative("mwcToast", "5000", "Could not refresh library. Please try Finish again.")
			continue
		}
		return
	}
}

func (s *receiver) closeHotspotUI() {
	s.hotspotUIMu.Lock()
	done := s.hotspotUIDone
	s.hotspotUIMu.Unlock()
	if done == nil {
		return
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
	}
	_ = hotspotNative("dlgConfirmClose")
	count, total, files := s.receivedSnapshot(0)
	body := fmt.Sprintf("<p>%d books received · %s</p>", count, humanSize(total))
	for i, f := range files {
		if i == 20 {
			body += "<p>Remaining books are in Received Books.</p>"
			break
		}
		body += "<p>" + html.EscapeString(f.Name) + " · " + f.SizeLabel + "</p>"
	}
	if count == 0 {
		body += "<p>No books received.</p>"
	}
	_ = hotspotNative("dlgConfirmAccept", "Session finished", body, "OK")
}
