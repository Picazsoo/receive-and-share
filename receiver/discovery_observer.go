package main

import (
	"context"
	"log"
	"net"
	"regexp"
	"strings"
	"time"

	"github.com/brutella/dnssd"
)

var advertisedLabel = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

// Match our per-advertising-run identifier as well as interface, address and port.
// The identifier is public discovery metadata, independent of authentication.
func ownAdvertisement(e dnssd.BrowseEntry, ip string, port int, identity string) string {
	if e.Text["receiver-id"] != identity || e.Port != port || e.IfaceName != "wlan0" || strings.ToLower(strings.TrimSuffix(e.Domain, ".")) != "local" {
		return ""
	}
	host := strings.ToLower(e.Host)
	if !advertisedLabel.MatchString(host) {
		return ""
	}
	for _, candidate := range e.IPs {
		if candidate.Equal(net.ParseIP(ip)) {
			return host + ".local"
		}
	}
	return ""
}

type serviceBrowser func(context.Context, string, dnssd.AddFunc, dnssd.RmvFunc) error

func (s *receiver) observeAdvertisement(ctx context.Context, ip string, port int, identity string) {
	s.observeAdvertisementUsing(ctx, ip, port, identity, dnssd.LookupType)
}

// Browse the wire through the unmodified dependency's public API. Fresh browse
// windows are necessary: LookupType's add callback does not report SRV target
// changes when a host conflict leaves the service-instance name unchanged.
func (s *receiver) observeAdvertisementUsing(ctx context.Context, ip string, port int, identity string, browse serviceBrowser) {
	publish := func(host, status string) {
		if ctx.Err() != nil {
			return
		}
		url := address(ip, port)
		if host != "" {
			url = address(host, port)
		}
		s.netMu.RLock()
		changed := s.url != url || s.mdnsStatus != status
		s.netMu.RUnlock()
		if changed {
			s.setNetwork(ip, host, status, port)
			log.Printf("Local address: %s", url)
		}
	}
	for ctx.Err() == nil {
		window, cancel := context.WithTimeout(ctx, 4*time.Second)
		found := false
		browse(window, "_http._tcp.local.", func(e dnssd.BrowseEntry) {
			if host := ownAdvertisement(e, ip, port, identity); host != "" {
				found = true
				publish(host, "Local name advertised. Use the numeric address if your browser cannot resolve it.")
			}
		}, func(dnssd.BrowseEntry) {})
		cancel()
		if !found {
			publish("", "Local name not confirmed; use the numeric address.")
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
