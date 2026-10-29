package main

import (
	"github.com/brutella/dnssd"
	"github.com/miekg/dns"
	"strings"
	"testing"
)

func TestAcceptedDeviceNameFitsDNS(t *testing.T) {
	for _, length := range []int{1, 47, 48} {
		name, err := normalizeName(strings.Repeat("a", length))
		if err != nil {
			t.Fatal(err)
		}
		instance := serviceInstanceName(name)
		if len(instance) > 55 {
			t.Fatal("no room for conflict suffix", instance)
		}
		for _, suffix := range []string{"", " (99999)"} {
			service, err := dnssd.NewService(dnssd.Config{Name: instance + suffix, Host: name, Type: "_http._tcp", Port: 8080})
			if err != nil {
				t.Fatal(err)
			}
			msg := new(dns.Msg)
			msg.SetQuestion(service.EscapedServiceInstanceName(), dns.TypeANY)
			if _, err = msg.Pack(); err != nil {
				t.Fatal(length, suffix, err)
			}
		}
	}
}

func TestConflictHostnameUpdatesNetworkAccess(t *testing.T) {
	s := fixture(t)
	for _, host := range []string{"kobo-demo.local", "kobo-demo-2.local"} {
		s.setNetwork("192.168.1.2", host, "advertised", 8080)
		if s.url != address(host, 8080) || !s.allowedHosts[host+":8080"] {
			t.Fatal(s.url, s.allowedHosts)
		}
	}
	// Numeric fallback remains usable while a conflict is being probed.
	s.setNetwork("192.168.1.2", "", "probing", 8080)
	if s.url != "http://192.168.1.2:8080/" {
		t.Fatal(s.url)
	}
}
