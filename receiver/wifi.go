package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

func readForceWiFi(file string) (bool, error) {
	f, e := os.Open(file)
	if e != nil {
		return false, e
	}
	defer f.Close()
	section := ""
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.Trim(line, "[]")
			continue
		}
		if section != "DeveloperSettings" {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 && strings.TrimSpace(parts[0]) == "ForceWifiOn" {
			return strconv.ParseBool(strings.TrimSpace(parts[1]))
		}
	}
	return false, scanner.Err()
}
func wifiCall(method string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	command := append([]string{"-m", method}, args...)
	b, e := exec.CommandContext(ctx, "/usr/bin/qndb", command...).CombinedOutput()
	if e != nil {
		return fmt.Errorf("%s: %w: %s", method, e, b)
	}
	return nil
}

type wifiLease struct {
	mu      sync.Mutex
	call    func(string, ...string) error
	marker  string
	restore bool
}

func (l *wifiLease) cycle() error {
	// These action names mean Wi-Fi enable/disable, despite the DBus method name.
	off := l.call("wfmSetAirplaneMode", "disable")
	// Give Nickel's asynchronous power transition a moment before enabling again.
	time.Sleep(500 * time.Millisecond)
	on := l.call("wfmSetAirplaneMode", "enable")
	if on != nil {
		return on
	}
	connect := l.call("wfmConnectWirelessSilently")
	if off != nil {
		return off
	}
	return connect
}
func acquireWiFi(config, marker string, call func(string, ...string) error) (*wifiLease, error) {
	original, e := readForceWiFi(config)
	if e != nil {
		return nil, e
	}
	var saved struct {
		Original bool `json:"original"`
		Version  int  `json:"version"`
	}
	if b, err := os.ReadFile(marker); err == nil {
		if json.Unmarshal(b, &saved) != nil || saved.Version != 1 {
			return nil, fmt.Errorf("invalid Wi-Fi restoration marker")
		}
		original = saved.Original
	} else if !os.IsNotExist(err) {
		return nil, err
	} else if !original {
		if e := os.MkdirAll(filepath.Dir(marker), 0755); e != nil {
			return nil, e
		}
		f, e := os.OpenFile(marker, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil {
			return nil, e
		}
		saved.Original = original
		saved.Version = 1
		e = json.NewEncoder(f).Encode(saved)
		if e == nil {
			e = f.Sync()
		}
		closeErr := f.Close()
		if e == nil {
			e = closeErr
		}
		if e != nil {
			os.Remove(marker)
			return nil, e
		}
	}
	lease := &wifiLease{call: call, marker: marker, restore: !original}
	if !lease.restore {
		return lease, nil
	}
	if e := call("nsForceWifi", "enable"); e != nil {
		return lease, e
	}
	return lease, nil
}
func (l *wifiLease) release() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.restore {
		return nil
	}
	if e := l.call("nsForceWifi", "disable"); e != nil {
		return e
	}
	if e := l.cycle(); e != nil {
		return e
	}
	if e := os.Remove(l.marker); e != nil && !os.IsNotExist(e) {
		return e
	}
	l.restore = false
	return nil
}
func (s *receiver) holdWiFi() func() {
	marker := filepath.Join(filepath.Dir(s.settingsPath), "wifi-restore.json")
	lease, e := acquireWiFi("/mnt/onboard/.kobo/Kobo/Kobo eReader.conf", marker, wifiCall)
	if e != nil {
		log.Printf("Could not retain Wi-Fi: %v", e)
	}
	if lease == nil {
		return func() {}
	}
	if e == nil && lease.restore {
		// Keep startup and the local browser responsive during the Wi-Fi reconnect.
		lease.mu.Lock()
		go func() {
			defer lease.mu.Unlock()
			if e := lease.cycle(); e != nil {
				log.Printf("Wi-Fi reconnect: %v", e)
			} else {
				log.Print("Wi-Fi idle power saving disabled for this session")
			}
		}()
	}
	return func() {
		if e := lease.release(); e != nil {
			log.Printf("Wi-Fi preference restoration failed (will retry next session): %v", e)
		} else {
			log.Print("Previous Wi-Fi power-saving preference restored")
		}
	}
}
