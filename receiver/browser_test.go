package main

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestBrowserLeaseRequiresWorkingHeartbeatAndGrace(t *testing.T) {
	s := fixture(t)
	now := time.Now()
	if s.browserExpired(now.Add(time.Hour)) {
		t.Fatal("unused browser armed itself")
	}
	s.noteBrowser(now)
	if s.browserExpired(now.Add(time.Minute)) {
		t.Fatal("single heartbeat armed auto finish")
	}
	s.noteBrowser(now.Add(3 * time.Second))
	if s.browserExpired(now.Add(22 * time.Second)) {
		t.Fatal("finished before grace expired")
	}
	if !s.browserExpired(now.Add(24 * time.Second)) {
		t.Fatal("missing heartbeat did not expire")
	}
	if s.browserExpired(now.Add(time.Hour)) {
		t.Fatal("lease fired twice")
	}
	s.noteBrowser(now.Add(30 * time.Second))
	s.noteBrowser(now.Add(33 * time.Second))
	s.noteBrowser(now.Add(50 * time.Second))
	if s.browserExpired(now.Add(60 * time.Second)) {
		t.Fatal("reopened page did not renew grace")
	}
}
func TestBrowserHeartbeatOnlyLocalWithToken(t *testing.T) {
	s := fixture(t)
	s.autoFinish = true
	for _, tc := range []struct {
		remote, token string
		code          int
	}{{"192.168.1.3:1234", s.token, 404}, {"127.0.0.1:1234", "wrong", 403}, {"127.0.0.1:1234", s.token, 200}} {
		r := httptest.NewRequest("POST", "http://kobo/local-heartbeat", strings.NewReader("token="+tc.token))
		r.RemoteAddr = tc.remote
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != tc.code {
			t.Fatal(w.Code, w.Body)
		}
	}
}
