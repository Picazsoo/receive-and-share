package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func wifiFixture(t *testing.T, enabled bool) (string, string) {
	t.Helper()
	dir := t.TempDir()
	config := filepath.Join(dir, "reader.conf")
	if e := os.WriteFile(config, []byte(fmt.Sprintf("[Other]\nForceWifiOn=true\n[DeveloperSettings]\nForceWifiOn=%t\n", enabled)), 0600); e != nil {
		t.Fatal(e)
	}
	return config, filepath.Join(dir, "wifi-restore.json")
}
func TestTemporaryWiFiRestoresOriginalPreference(t *testing.T) {
	config, marker := wifiFixture(t, false)
	calls := []string{}
	call := func(method string, args ...string) error {
		calls = append(calls, method+":"+strings.Join(args, ","))
		return nil
	}
	lease, e := acquireWiFi(config, marker, call)
	if e != nil || !lease.restore {
		t.Fatal(e)
	}
	if e := lease.cycle(); e != nil {
		t.Fatal(e)
	}
	if e := lease.release(); e != nil {
		t.Fatal(e)
	}
	expected := []string{"nsForceWifi:enable", "wfmSetAirplaneMode:disable", "wfmSetAirplaneMode:enable", "wfmConnectWirelessSilently:", "nsForceWifi:disable", "wfmSetAirplaneMode:disable", "wfmSetAirplaneMode:enable", "wfmConnectWirelessSilently:"}
	if strings.Join(calls, "|") != strings.Join(expected, "|") {
		t.Fatal(calls)
	}
	if _, e := os.Stat(marker); !os.IsNotExist(e) {
		t.Fatal("restoration marker not removed")
	}
	config, marker = wifiFixture(t, true)
	calls = nil
	lease, e = acquireWiFi(config, marker, call)
	if e != nil || lease.restore {
		t.Fatal(e)
	}
	lease.release()
	if len(calls) != 0 {
		t.Fatal("changed a preexisting ForceWifiOn preference", calls)
	}
}
func TestWiFiRestorationSurvivesInterruptedSession(t *testing.T) {
	config, marker := wifiFixture(t, false)
	call := func(string, ...string) error { return nil }
	if _, e := acquireWiFi(config, marker, call); e != nil {
		t.Fatal(e)
	}
	os.WriteFile(config, []byte("[DeveloperSettings]\nForceWifiOn=true\n"), 0600)
	lease, e := acquireWiFi(config, marker, call)
	if e != nil || !lease.restore {
		t.Fatal("lost original preference after interrupted session", e)
	}
	lease.call = func(method string, _ ...string) error {
		if method == "nsForceWifi" {
			return fmt.Errorf("temporary DBus error")
		}
		return nil
	}
	if e := lease.release(); e == nil {
		t.Fatal("restoration failure ignored")
	}
	if _, e := os.Stat(marker); e != nil {
		t.Fatal("restoration journal lost", e)
	}
	lease.call = call
	if e := lease.release(); e != nil {
		t.Fatal(e)
	}
}
