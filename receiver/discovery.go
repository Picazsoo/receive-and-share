package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/brutella/dnssd"
	qrcode "github.com/skip2/go-qrcode"
)

var validDeviceName = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,46}[a-z0-9])?$`)

func normalizeName(name string) (string, error) {
	name = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(name)), ".local")
	if !validDeviceName.MatchString(name) {
		return "", fmt.Errorf("use 1–48 lowercase letters, numbers or hyphens; start and end with a letter or number")
	}
	return name, nil
}

type deviceSettings struct {
	Name     string `json:"name"`
	ShareAll bool   `json:"shareAll"`
}

func saveSettings(file string, cfg deviceSettings) error {
	if err := os.MkdirAll(filepath.Dir(file), 0755); err != nil {
		return err
	}
	b, _ := json.Marshal(cfg)
	f, err := os.CreateTemp(filepath.Dir(file), ".device-name-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	_, err = f.Write(b)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(tmp, file)
}
func loadSettings(file string) (deviceSettings, error) {
	b, err := os.ReadFile(file)
	if err == nil {
		var cfg deviceSettings
		if json.Unmarshal(b, &cfg) != nil {
			return cfg, fmt.Errorf("invalid device-name settings")
		}
		cfg.Name, err = normalizeName(cfg.Name)
		return cfg, err
	}
	if !os.IsNotExist(err) {
		return deviceSettings{}, err
	}
	name := "kobo-" + secret()[:8]
	cfg := deviceSettings{Name: name}
	return cfg, saveSettings(file, cfg)
}
func loadDeviceName(file string) (string, error) { cfg, e := loadSettings(file); return cfg.Name, e }
func saveDeviceName(file, name string) error {
	cfg := deviceSettings{}
	if b, e := os.ReadFile(file); e == nil {
		if e = json.Unmarshal(b, &cfg); e != nil {
			return e
		}
	}
	cfg.Name = name
	return saveSettings(file, cfg)
}
func (s *receiver) rename(w http.ResponseWriter, r *http.Request) {
	if !local(r) {
		http.NotFound(w, r)
		return
	}
	if r.Method != "POST" {
		http.Error(w, "POST required", 405)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 2048)
	if r.ParseForm() != nil || !equal(r.PostForm.Get("token"), s.token) {
		http.Error(w, "Open Receive & Share again", 403)
		return
	}
	name, err := normalizeName(r.PostForm.Get("name"))
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	s.settingsMu.Lock()
	err = saveDeviceName(s.settingsPath, name)
	s.settingsMu.Unlock()
	if err != nil {
		http.Error(w, "Could not save device name", 500)
		return
	}
	s.netMu.Lock()
	s.deviceName = name
	s.netMu.Unlock()
	s.localRender(w, localPageHead+backToConnection+`<h2>Device name saved</h2><p>{{.}}.local will be used the next time you start Receive & Share. The current session and its address stay active.</p></body></html>`, name)
}
func address(host string, port int) string {
	if port == 80 {
		return "http://" + host + "/"
	}
	return fmt.Sprintf("http://%s:%d/", host, port)
}
func (s *receiver) setNetwork(ip, host, status string, port int) {
	s.netMu.Lock()
	defer s.netMu.Unlock()
	s.fallbackURL = address(ip, port)
	s.url = s.fallbackURL
	if host != "" {
		s.url = address(host, port)
	}
	s.mdnsStatus = status
	// Retain previous numeric addresses for already-loaded clients after DHCP changes.
	if s.allowedHosts == nil {
		s.allowedHosts = map[string]bool{}
	}
	for _, h := range []string{ip, host, "127.0.0.1"} {
		if h != "" {
			s.allowedHosts[h] = true
			s.allowedHosts[fmt.Sprintf("%s:%d", h, port)] = true
		}
	}
	if png, e := qrcode.Encode(s.url, qrcode.Medium, 420); e == nil {
		s.qr = png
	}
}

// Restart advertising after interface/address changes; HTTP cookies and upload IDs
// remain in memory while Wi-Fi is absent. DNS errors never stop the HTTP server.
func (s *receiver) advertise(ctx context.Context, port int, ready chan struct{}) {
	s.netMu.RLock()
	name := s.deviceName
	s.netMu.RUnlock()
	var cancel context.CancelFunc
	var stopped chan struct{}
	lastIP := ""
	lastTry := time.Time{}
	stop := func() {
		if cancel != nil {
			cancel()
			select {
			case <-stopped:
			case <-time.After(3 * time.Second):
			}
			cancel = nil
		}
	}
	defer stop()
	tick := time.NewTicker(3 * time.Second)
	defer tick.Stop()
	for {
		ip, err := wifiIP()
		if err != nil {
			if lastIP != "" {
				stop()
				lastIP = ""
				s.netMu.Lock()
				s.mdnsStatus = "Wi-Fi disconnected. Reconnect Wi-Fi to resume this session."
				s.netMu.Unlock()
			}
		} else if ip != lastIP || (cancel == nil && time.Since(lastTry) > 30*time.Second) {
			stop()
			lastIP = ip
			lastTry = time.Now()
			s.setNetwork(ip, "", "Checking the local network name…", port)
			probeCtx, doneProbe := context.WithTimeout(ctx, 8*time.Second)
			service, e := dnssd.NewService(dnssd.Config{Name: "Receive & Share " + name, Host: name, Type: "_http._tcp", Port: port, Ifaces: []string{"wlan0"}, IPs: []net.IP{net.ParseIP(ip)}, Text: map[string]string{"path": "/"}})
			if e == nil {
				service, e = dnssd.ProbeService(probeCtx, service)
			}
			doneProbe()
			if e != nil {
				s.setNetwork(ip, "", "Local name unavailable; use the numeric address.", port)
				log.Printf("mDNS unavailable: %v", e)
			} else {
				responder, e := dnssd.NewResponder()
				if e == nil {
					_, e = responder.Add(service)
				}
				if e != nil {
					s.setNetwork(ip, "", "Local name unavailable; use the numeric address.", port)
					log.Printf("mDNS startup: %v", e)
				} else {
					var runCtx context.Context
					runCtx, cancel = context.WithCancel(ctx)
					stopped = make(chan struct{})
					finished := stopped
					go func() {
						defer close(finished)
						if e := responder.Respond(runCtx); e != nil && runCtx.Err() == nil {
							log.Printf("mDNS responder: %v", e)
						}
					}()
					host := strings.TrimSuffix(service.Hostname(), ".")
					s.setNetwork(ip, host, "Local name advertised. Use the numeric address if your browser cannot resolve it.", port)
					log.Printf("Local address: %s", address(host, port))
				}
			}
		}
		if stopped != nil && cancel != nil {
			select {
			case <-stopped:
				cancel = nil
			default:
			}
		}
		if ready != nil {
			close(ready)
			ready = nil
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}
