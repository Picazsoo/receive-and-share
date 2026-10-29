package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

type reviewListener struct {
	conn   net.Conn
	closed chan struct{}
	once   sync.Once
}

func (l *reviewListener) Accept() (net.Conn, error) {
	if l.conn != nil {
		c := l.conn
		l.conn = nil
		return c, nil
	}
	<-l.closed
	return nil, net.ErrClosed
}
func (l *reviewListener) Close() error { l.once.Do(func() { close(l.closed) }); return nil }
func (*reviewListener) Addr() net.Addr { return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 8080} }

type reviewLocalConn struct{ net.Conn }

func (reviewLocalConn) RemoteAddr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1234}
}

func TestFinishAfterSlowTransfer(t *testing.T) {
	s := fixture(t)
	s.onFinish = func() error { return nil }
	synctest.Test(t, func(t *testing.T) {
		client, serverConn := net.Pipe()
		listener := &reviewListener{conn: reviewLocalConn{serverConn}, closed: make(chan struct{})}
		server := &http.Server{Handler: s, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second}
		defer server.Close()
		defer client.Close()
		go server.Serve(listener)
		s.uploads.Add(1)
		go func() { time.Sleep(6 * time.Minute); s.uploads.Done() }()
		body := "token=" + s.token
		fmt.Fprintf(client, "POST /finish HTTP/1.1\r\nHost: 127.0.0.1:8080\r\nContent-Type: application/x-www-form-urlencoded\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s", len(body), body)
		response, err := io.ReadAll(client)
		if err != nil {
			t.Fatal(err)
		}
		server.Shutdown(context.Background())
		t.Logf("scan succeeded=%t; confirmation returned after six-minute transfer=%t", s.finished, strings.Contains(string(response), "Session finished"))
		if !strings.Contains(string(response), "Session finished") {
			t.Fatal("Finish lost its confirmation because its five-minute write deadline expired while waiting")
		}
	})
}
