package main

import (
	"context"
	"github.com/brutella/dnssd"
	"net"
	"testing"
	"testing/synctest"
)

func TestObservedAdvertisementTracksHostRename(t *testing.T) {
	s := fixture(t)
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		calls := 0
		browse := func(_ context.Context, service string, add dnssd.AddFunc, _ dnssd.RmvFunc) error {
			calls++
			if service != "_http._tcp.local." {
				t.Fatal(service)
			}
			host := "kobo-demo"
			if calls == 2 {
				host = "kobo-demo-2"
			}
			add(dnssd.BrowseEntry{Host: host, Domain: "local", IfaceName: "wlan0", IPs: []net.IP{net.ParseIP("192.168.1.2")}, Port: 8080, Text: map[string]string{"receiver-id": "identity"}})
			if s.url != address(host+".local", 8080) || !s.allowedHosts[host+".local:8080"] {
				t.Fatal(s.url, s.allowedHosts)
			}
			if calls == 2 {
				cancel()
			}
			return nil
		}
		s.observeAdvertisementUsing(ctx, "192.168.1.2", 8080, "identity", browse)
		if calls != 2 {
			t.Fatal(calls)
		}
	})
}

func TestDiscoveryIgnoresOtherServices(t *testing.T) {
	entry := dnssd.BrowseEntry{Host: "demo-2", Domain: "local", IfaceName: "wlan0", IPs: []net.IP{net.ParseIP("192.168.1.2")}, Port: 8080, Text: map[string]string{"receiver-id": "identity"}}
	if got := ownAdvertisement(entry, "192.168.1.2", 8080, "identity"); got != "demo-2.local" {
		t.Fatal(got)
	}
	for _, change := range []func(*dnssd.BrowseEntry){
		func(e *dnssd.BrowseEntry) { e.Text = map[string]string{"receiver-id": "different"} },
		func(e *dnssd.BrowseEntry) { e.IPs = []net.IP{net.ParseIP("192.168.1.3")} },
		func(e *dnssd.BrowseEntry) { e.Port = 80 },
		func(e *dnssd.BrowseEntry) { e.IfaceName = "other" },
		func(e *dnssd.BrowseEntry) { e.Domain = "example.org" },
		func(e *dnssd.BrowseEntry) { e.Host = "invalid.example" },
	} {
		other := entry
		change(&other)
		if host := ownAdvertisement(other, "192.168.1.2", 8080, "identity"); host != "" {
			t.Fatal("accepted foreign advertisement", host)
		}
	}
}
