package main

import (
	"net"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestHotspotWithoutStationCarrierIsHealthy(t *testing.T) {
	_, address, _ := net.ParseCIDR(hotspotIP + "/24")
	address.IP = net.ParseIP(hotspotIP)
	if err := checkHotspotInterface(net.Interface{Flags: net.FlagUp}, []net.Addr{address}); err != nil {
		t.Fatal(err)
	}
	if checkHotspotInterface(net.Interface{}, []net.Addr{address}) == nil {
		t.Fatal("down radio accepted")
	}
	if checkHotspotInterface(net.Interface{Flags: net.FlagUp}, nil) == nil {
		t.Fatal("missing address accepted")
	}
}

func TestOldHotspotGuardCannotRestoreNewSession(t *testing.T) {
	original := hotspotDir
	hotspotDir = t.TempDir()
	t.Cleanup(func() { hotspotDir = original })
	replacement := hotspotState{Parent: hotspotProcess{PID: 12345, Generation: "replacement"}}
	if err := saveHotspotState(replacement); err != nil {
		t.Fatal(err)
	}
	old := hotspotProcess{PID: 12345, Generation: "old"}
	if err := restoreHotspot(old); err != nil {
		t.Fatal(err)
	}
	if err := hotspotGuard("12345", "old"); err != nil {
		t.Fatal(err)
	}
	got, err := readHotspotState()
	if err != nil || got.Parent != replacement.Parent {
		t.Fatal("replacement session was changed", got, err)
	}
	if _, err = os.Stat(hotspotDir + "/restoring"); !os.IsNotExist(err) {
		t.Fatal("old guard took a restoration lock", err)
	}
}

func TestHotspotCredentialsAreLocalOnly(t *testing.T) {
	s := fixture(t)
	s.hotspot = &hotspotNetwork{SSID: "Kobo-Share-test", Password: "private-test-password", QR: []byte("test-png")}
	for _, endpoint := range []string{"/screen?connection=wifi", "/wifi-qr.png"} {
		r := httptest.NewRequest("GET", "http://kobo"+endpoint, nil)
		r.RemoteAddr = "127.0.0.1:1234"
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body)
		}
		if strings.Contains(endpoint, "screen") && (!strings.Contains(w.Body.String(), s.hotspot.Password) || !strings.Contains(w.Body.String(), "Next: open transfer page")) {
			t.Fatal(w.Body)
		}
		r = httptest.NewRequest("GET", "http://kobo"+endpoint, nil)
		r.RemoteAddr = "192.168.42.10:1234"
		r.Header.Set("Cookie", "kobo_session=test-cookie")
		w = httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if strings.Contains(w.Body.String(), s.hotspot.Password) || strings.Contains(w.Body.String(), "test-png") || w.Code == 200 {
			t.Fatal("hotspot credentials exposed remotely", w.Code, w.Body)
		}
	}
	r := httptest.NewRequest("GET", "http://kobo/screen", nil)
	r.RemoteAddr = "127.0.0.1:1234"
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if !strings.Contains(w.Body.String(), "Wi-Fi connection QR") || strings.Contains(w.Body.String(), s.hotspot.Password) {
		t.Fatal(w.Body)
	}
	// Moving from Wi-Fi joining to the transfer screen explicitly renews pairing.
	s.pinUntil = time.Now().Add(-time.Minute)
	r = httptest.NewRequest("POST", "http://kobo/renew", strings.NewReader("token="+s.token))
	r.RemoteAddr = "127.0.0.1:1234"
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 303 || time.Now().After(s.pinUntil) {
		t.Fatal(w.Code, w.Body)
	}
}

func TestWiFiQREscapesSpecialCharacters(t *testing.T) {
	got := wifiQRText(`a;b:c,d\e"f`, `p;q:r,s\t"u`)
	want := `WIFI:T:WPA;S:a\;b\:c\,d\\e\"f;P:p\;q\:r\,s\\t\"u;;`
	if got != want {
		t.Fatalf("%q != %q", got, want)
	}
}

func TestHotspotProcessIdentityParsing(t *testing.T) {
	fields := append([]string{"S"}, strings.Fields("1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 987654")...)
	stat := "123 (a name with ) parentheses) " + strings.Join(fields, " ")
	if got := parseProcessGeneration(stat); got != "987654" {
		t.Fatal(got)
	}
	if parseProcessGeneration(strings.Replace(stat, ") S ", ") Z ", 1)) != "" {
		t.Fatal("zombie treated as alive")
	}
	if parseProcessGeneration("truncated") != "" {
		t.Fatal("invalid identity accepted")
	}
}

func TestHotspotSkipsPINButKeepsCSRFAndNetworkBoundary(t *testing.T) {
	s := fixture(t)
	s.hotspot = &hotspotNetwork{SSID: "test", Password: "private"}
	s.shareAll = true
	seed(t, s, "Books/test.pdf")
	r := httptest.NewRequest("GET", "http://kobo/basic", nil)
	r.RemoteAddr = "192.168.42.10:1234"
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "test.pdf") || len(w.Result().Cookies()) != 1 {
		t.Fatal(w.Code, w.Body)
	}
	cookie := w.Result().Cookies()[0]
	r = httptest.NewRequest("GET", "http://kobo/api/session", nil)
	r.RemoteAddr = "192.168.42.10:1234"
	r.AddCookie(cookie)
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"shareAll":true`) || !strings.Contains(w.Body.String(), `"allowDelete":false`) {
		t.Fatal(w.Code, w.Body)
	}
	r = httptest.NewRequest("POST", "http://kobo/api/delete-review", strings.NewReader("file=Books/test.pdf"))
	r.RemoteAddr = "192.168.42.10:1234"
	r.AddCookie(cookie)
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("POST without CSRF accepted", w.Code)
	}
	for _, peer := range []string{"192.168.0.10:1234", "169.254.235.128:1234"} {
		r = httptest.NewRequest("GET", "http://kobo/basic", nil)
		r.RemoteAddr = peer
		r.AddCookie(cookie)
		w = httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatal("other network accepted", peer, w.Code)
		}
	}
	r = httptest.NewRequest("GET", "http://kobo/", nil)
	r.RemoteAddr = "192.168.42.11:1234"
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if len(w.Result().Cookies()) > 0 {
		t.Fatal("cross-site navigation granted a session")
	}
}
