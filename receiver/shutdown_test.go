package main

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

func TestRejectedHostDoesNotWaitForMissingBody(t *testing.T) {
	s := fixture(t)
	s.allowedHosts = map[string]bool{"127.0.0.1:8080": true}
	synctest.Test(t, func(t *testing.T) {
		client, conn := net.Pipe()
		listener := &reviewListener{conn: reviewLocalConn{conn}, closed: make(chan struct{})}
		server := &http.Server{Handler: s}
		defer server.Close()
		defer client.Close()
		go server.Serve(listener)
		fmt.Fprint(client, "POST / HTTP/1.1\r\nHost: rejected.example\r\nContent-Length: 1\r\n\r\n")
		before := time.Now()
		response, err := io.ReadAll(client)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(response), "403 Forbidden") {
			t.Fatal(string(response))
		}
		shutdownReceiver(server, s)
		if time.Since(before) > time.Second {
			t.Fatal("rejected body delayed shutdown", time.Since(before))
		}
	})
}

func TestShutdownBoundsUnfinishedNonTransferRequests(t *testing.T) {
	s := fixture(t)
	synctest.Test(t, func(t *testing.T) {
		client, conn := net.Pipe()
		listener := &reviewListener{conn: reviewLocalConn{conn}, closed: make(chan struct{})}
		entered := make(chan struct{})
		server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(entered); io.Copy(io.Discard, r.Body) })}
		defer server.Close()
		defer client.Close()
		go server.Serve(listener)
		fmt.Fprint(client, "POST / HTTP/1.1\r\nHost: 127.0.0.1:8080\r\nContent-Length: 1\r\n\r\n")
		<-entered
		before := time.Now()
		shutdownReceiver(server, s)
		if elapsed := time.Since(before); elapsed < 5*time.Second || elapsed > 6*time.Second {
			t.Fatal(elapsed)
		}
		// net/http briefly lingers after force-closing an unread request body.
		time.Sleep(time.Second)
	})
}

func TestShutdownPreservesProgressingTransfers(t *testing.T) {
	s := fixture(t)
	synctest.Test(t, func(t *testing.T) {
		server := &http.Server{}
		s.uploads.Add(1)
		go func() { time.Sleep(6 * time.Minute); s.uploads.Done() }()
		before := time.Now()
		shutdownReceiver(server, s)
		if time.Since(before) != 6*time.Minute {
			t.Fatal("transfer interrupted", time.Since(before))
		}
	})
}
