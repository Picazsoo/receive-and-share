package main

// Hotspot helpers deliberately use temporary root-owned state, not book storage.
// A separate guard restores networking even if the receiver is killed abruptly.
import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	qrcode "github.com/skip2/go-qrcode"
)

var hotspotDir = "/tmp/kobo-receive-hotspot"

const hotspotIP = "192.168.42.1"
const hotspotDriverDir = "/drivers/mx6sll-ntx/wifi"

type hotspotNetwork struct {
	SSID, Password string
	QR             []byte
}
type hotspotProcess struct {
	PID        int
	Generation string
}
type hotspotState struct {
	Parent                                       hotspotProcess
	Deadline                                     time.Time
	OriginalForce, OriginalOn, OriginalConnected bool
	ManualRadio                                  bool
	Children                                     []hotspotProcess
}

func processGeneration(pid int) string {
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return ""
	}
	return parseProcessGeneration(string(b))
}
func parseProcessGeneration(stat string) string {
	// comm may contain spaces and parentheses; starttime is field 22.
	end := strings.LastIndexByte(stat, ')')
	if end < 0 {
		return ""
	}
	fields := strings.Fields(stat[end+1:])
	if len(fields) < 20 || fields[0] == "Z" {
		return ""
	}
	return fields[19]
}
func (p hotspotProcess) alive() bool {
	return p.PID > 1 && p.Generation != "" && processGeneration(p.PID) == p.Generation
}
func currentHotspotProcess(pid int) hotspotProcess {
	return hotspotProcess{pid, processGeneration(pid)}
}

func saveHotspotState(s hotspotState) error {
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	if err = os.WriteFile(hotspotDir+"/state.new", b, 0600); err != nil {
		return err
	}
	return os.Rename(hotspotDir+"/state.new", hotspotDir+"/state.json")
}
func readHotspotState() (hotspotState, error) {
	var s hotspotState
	b, e := os.ReadFile(hotspotDir + "/state.json")
	if e == nil {
		e = json.Unmarshal(b, &s)
	}
	return s, e
}
func hotspotCommand(name string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	b, e := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if e != nil {
		return fmt.Errorf("%s failed: %w: %s", filepath.Base(name), e, b)
	}
	return nil
}
func hotspotNative(method string, args ...string) error {
	return hotspotCommand("/usr/bin/qndb", append([]string{"-m", method}, args...)...)
}

func wifiQRText(ssid, password string) string {
	escape := func(s string) string {
		return strings.NewReplacer(`\`, `\\`, `;`, `\;`, `,`, `\,`, `:`, `\:`, `"`, `\"`).Replace(s)
	}
	return "WIFI:T:WPA;S:" + escape(ssid) + ";P:" + escape(password) + ";;"
}

func hotspotPassword() string {
	// Three pronounceable CVCVC groups, lowercase only: about 51 bits of
	// randomness. Credentials are regenerated for each short-lived session.
	const consonants = "bcdfghjkmnprstvwz"
	const vowels = "aeiou"
	groups := make([]string, 3)
	for i := range groups {
		word := make([]byte, 5)
		for j := range word {
			alphabet := consonants
			if j%2 == 1 {
				alphabet = vowels
			}
			n, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
			if err != nil {
				panic(err)
			}
			word[j] = alphabet[n.Int64()]
		}
		groups[i] = string(word)
	}
	return strings.Join(groups, "-")
}

func startHotspot(ttl time.Duration) (*hotspotNetwork, func(), error) {
	if runtime.GOOS != "linux" {
		return nil, nil, fmt.Errorf("hotspot mode requires Kobo Linux")
	}
	executable, e := os.Executable()
	if e != nil {
		return nil, nil, e
	}
	app := filepath.Dir(executable)
	for _, name := range []string{"hostapd", "hotspot-dhcp"} {
		if st, err := os.Stat(filepath.Join(app, name)); err != nil || st.Mode()&0111 == 0 {
			return nil, nil, fmt.Errorf("missing hotspot helper %s; install the hotspot-enabled package", name)
		}
	}
	for _, name := range []string{"8723ds.ko", "sdio_wifi_pwr.ko"} {
		if _, err := os.Stat(filepath.Join(hotspotDriverDir, name)); err != nil {
			return nil, nil, fmt.Errorf("hotspot requires the Libra 2 stock RTL8723DS driver")
		}
	}
	if e = os.Mkdir(hotspotDir, 0700); e != nil {
		if previous, err := readHotspotState(); err == nil && !previous.Parent.alive() {
			_ = restoreHotspot(previous.Parent)
		}
		return nil, nil, fmt.Errorf("hotspot is active or recovering; wait and try again: %w", e)
	}
	force, e := readForceWiFi("/mnt/onboard/.kobo/Kobo/Kobo eReader.conf")
	if e != nil {
		os.Remove(hotspotDir)
		return nil, nil, e
	}
	iface, _ := net.InterfaceByName("wlan0")
	on := iface != nil && iface.Flags&net.FlagUp != 0
	_, connectedErr := wifiIP()
	state := hotspotState{Parent: currentHotspotProcess(os.Getpid()), Deadline: time.Now().Add(ttl + 2*time.Minute), OriginalForce: force, OriginalOn: on, OriginalConnected: connectedErr == nil}
	state.ManualRadio = true
	if state.Parent.Generation == "" {
		os.Remove(hotspotDir)
		return nil, nil, fmt.Errorf("cannot establish hotspot process identity")
	}
	if e = saveHotspotState(state); e != nil {
		os.Remove(hotspotDir)
		return nil, nil, e
	}
	monitorStop := make(chan struct{})
	cleanup := func() {
		close(monitorStop)
		if err := restoreHotspot(state.Parent); err != nil {
			log.Printf("Hotspot restoration: %v", err)
		}
	}
	fail := func(err error) (*hotspotNetwork, func(), error) {
		// Preserve bounded helper diagnostics before removing credentials and state.
		for _, name := range []string{"hostapd.log", "busybox.log"} {
			if b, readErr := os.ReadFile(hotspotDir + "/" + name); readErr == nil {
				if len(b) > 16384 {
					b = b[len(b)-16384:]
				}
				_ = os.WriteFile("/tmp/kobo-receive-hotspot-"+name, b, 0600)
			}
		}
		cleanup()
		return nil, nil, err
	}
	guard := exec.Command(executable, "__hotspot-guard", strconv.Itoa(state.Parent.PID), state.Parent.Generation)
	guard.Stdin = nil
	if e = guard.Start(); e != nil {
		return fail(e)
	}
	go guard.Wait()
	network := &hotspotNetwork{SSID: "Kobo-Share-" + secret()[:6], Password: hotspotPassword()}
	network.QR, e = qrcode.Encode(wifiQRText(network.SSID, network.Password), qrcode.Medium, 420)
	if e != nil {
		return fail(e)
	}
	// Leave Nickel offline so its station-connection timeout cannot power down
	// our AP. Use only the device's existing kernel modules, without replacing them.
	if e = hotspotNative("wfmSetAirplaneMode", "disable"); e != nil {
		return fail(e)
	}
	time.Sleep(2 * time.Second)
	_ = hotspotCommand("/bin/wpa_cli", "-i", "wlan0", "terminate")
	if e = unloadHotspotRadio(); e != nil {
		return fail(e)
	}
	for _, name := range []string{"sdio_wifi_pwr.ko", "8723ds.ko"} {
		if e = hotspotCommand("/sbin/insmod", filepath.Join(hotspotDriverDir, name)); e != nil {
			return fail(e)
		}
	}
	for i := 0; i < 15; i++ {
		if _, err := net.InterfaceByName("wlan0"); err == nil {
			break
		}
		time.Sleep(time.Second)
	}
	if e = hotspotCommand("/sbin/ifconfig", "lo", "127.0.0.1", "up"); e != nil {
		return fail(e)
	}
	if e = hotspotCommand("/sbin/ifconfig", "wlan0", "down"); e != nil {
		return fail(e)
	}
	if e = hotspotCommand("/sbin/ifconfig", "wlan0", hotspotIP, "netmask", "255.255.255.0", "up"); e != nil {
		return fail(e)
	}
	apConfig := "interface=wlan0\ndriver=nl80211\nssid=" + network.SSID + "\nhw_mode=g\nchannel=6\nwmm_enabled=1\nauth_algs=1\nwpa=2\nwpa_passphrase=" + network.Password + "\nwpa_key_mgmt=WPA-PSK\nrsn_pairwise=CCMP\nctrl_interface=" + hotspotDir + "/ctrl\n"
	dhcpConfig := "start 192.168.42.10\nend 192.168.42.100\ninterface wlan0\nmax_leases 91\nauto_time 30\nlease_file " + hotspotDir + "/leases\npidfile " + hotspotDir + "/dhcp.pid\noption subnet 255.255.255.0\noption lease 1200\n"
	if e = os.WriteFile(hotspotDir+"/hostapd.conf", []byte(apConfig), 0600); e != nil {
		return fail(e)
	}
	if e = os.WriteFile(hotspotDir+"/dhcp.conf", []byte(dhcpConfig), 0600); e != nil {
		return fail(e)
	}
	// BusyBox dispatches by argv[0]; the installed helper has a private busybox link.
	if e = os.Symlink(filepath.Join(app, "hotspot-dhcp"), hotspotDir+"/busybox"); e != nil {
		return fail(e)
	}
	start := func(name string, args ...string) error {
		output, err := os.OpenFile(hotspotDir+"/"+filepath.Base(name)+".log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
		if err != nil {
			return err
		}
		defer output.Close()
		cmd := exec.Command(name, args...)
		cmd.Stdout = output
		cmd.Stderr = output
		hotspotChildOptions(cmd)
		if err = cmd.Start(); err != nil {
			return err
		}
		state.Children = append(state.Children, currentHotspotProcess(cmd.Process.Pid))
		go cmd.Wait()
		return saveHotspotState(state)
	}
	if e = start(filepath.Join(app, "hostapd"), hotspotDir+"/hostapd.conf"); e != nil {
		return fail(e)
	}
	if e = start(hotspotDir+"/busybox", "udhcpd", "-f", hotspotDir+"/dhcp.conf"); e != nil {
		return fail(e)
	}
	// Wait for the AP to be enabled, not just for hostapd to have a PID.
	enabled := false
	for i := 0; i < 12; i++ {
		allAlive := true
		for _, p := range state.Children {
			allAlive = allAlive && p.alive()
		}
		if !allAlive {
			return fail(fmt.Errorf("hotspot helper stopped; see %s", hotspotDir))
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		b, _ := exec.CommandContext(ctx, "/bin/wpa_cli", "-p", hotspotDir+"/ctrl", "-i", "wlan0", "status").Output()
		cancel()
		if strings.Contains(string(b), "state=ENABLED") {
			enabled = true
			break
		}
		time.Sleep(time.Second)
	}
	if !enabled {
		return fail(fmt.Errorf("access point did not become ready"))
	}
	// A failed helper must stop the receiver so its normal deferred cleanup runs.
	go func() {
		misses := 0
		for {
			select {
			case <-monitorStop:
				return
			case <-time.After(time.Second):
			}
			if _, err := os.Stat(hotspotDir + "/state.json"); err != nil {
				return
			}
			if err := hotspotInterfaceReady(); err != nil {
				misses++
				log.Printf("Hotspot interface check (%d/3): %v", misses, err)
				if misses >= 3 {
					log.Print("Hotspot network interface lost; stopping the session")
					process, _ := os.FindProcess(os.Getpid())
					_ = process.Signal(os.Interrupt)
					return
				}
			} else {
				misses = 0
			}
			for _, p := range state.Children {
				if !p.alive() {
					process, _ := os.FindProcess(os.Getpid())
					_ = process.Signal(os.Interrupt)
					return
				}
			}
		}
	}()
	return network, cleanup, nil
}

func hotspotInterfaceReady() error {
	iface, err := net.InterfaceByName("wlan0")
	if err != nil {
		return err
	}
	addrs, err := iface.Addrs()
	if err != nil {
		return err
	}
	return checkHotspotInterface(*iface, addrs)
}

func checkHotspotInterface(iface net.Interface, addrs []net.Addr) error {
	// IFF_RUNNING/carrier is a station-link indication in this vendor driver.
	// An AP may be healthy and broadcasting before any station has associated.
	if iface.Flags&net.FlagUp == 0 {
		return fmt.Errorf("wlan0 is down (flags=%s)", iface.Flags)
	}
	for _, addr := range addrs {
		ip, _, err := net.ParseCIDR(addr.String())
		if err == nil && ip.String() == hotspotIP {
			return nil
		}
	}
	return fmt.Errorf("wlan0 lacks hotspot address (flags=%s, addresses=%v)", iface.Flags, addrs)
}

func restoreHotspot(expected hotspotProcess) error {
	s, e := readHotspotState()
	if os.IsNotExist(e) {
		return nil
	}
	if e != nil {
		return e
	}
	if s.Parent != expected {
		return nil
	}
	if e = os.Mkdir(hotspotDir+"/restoring", 0700); e != nil {
		// A restorer can itself be killed. Its lock must not strand the radio.
		b, err := os.ReadFile(hotspotDir + "/restoring/owner.json")
		var owner hotspotProcess
		if err == nil && json.Unmarshal(b, &owner) == nil && !owner.alive() {
			_ = os.RemoveAll(hotspotDir + "/restoring")
		} else if err != nil {
			if info, statErr := os.Stat(hotspotDir + "/restoring"); statErr == nil && time.Since(info.ModTime()) > 30*time.Second {
				_ = os.RemoveAll(hotspotDir + "/restoring")
			}
		}
		return nil
	}
	owner, _ := json.Marshal(currentHotspotProcess(os.Getpid()))
	if e = os.WriteFile(hotspotDir+"/restoring/owner.json", owner, 0600); e != nil {
		_ = os.Remove(hotspotDir + "/restoring")
		return e
	}
	// The previous session may have completed between reading state and taking
	// the lock. Never let its late guard restore a replacement session.
	s, e = readHotspotState()
	if e != nil || s.Parent != expected {
		_ = os.RemoveAll(hotspotDir + "/restoring")
		return e
	}
	success := false
	if !expected.alive() {
		if _, err := os.Stat(hotspotDir + "/ui-active"); err == nil {
			_ = hotspotNative("dlgConfirmClose")
		}
	}
	defer func() {
		if success {
			_ = os.RemoveAll(hotspotDir)
		} else {
			_ = os.RemoveAll(hotspotDir + "/restoring")
		}
	}()
	for _, p := range s.Children {
		if p.alive() {
			process, _ := os.FindProcess(p.PID)
			_ = process.Kill()
		}
	}
	_ = hotspotCommand("/sbin/ifconfig", "wlan0", "down")
	force := "disable"
	if s.OriginalForce {
		force = "enable"
	}
	if e = hotspotNative("nsForceWifi", force); e != nil {
		return e
	}
	if e = hotspotNative("wfmSetAirplaneMode", "disable"); e != nil {
		return e
	}
	if s.ManualRadio {
		time.Sleep(time.Second)
		if e = unloadHotspotRadio(); e != nil {
			return e
		}
	}
	if s.OriginalOn {
		time.Sleep(time.Second)
		if e = hotspotNative("wfmSetAirplaneMode", "enable"); e != nil {
			return e
		}
		if e = hotspotNative("wfmConnectWirelessSilently"); e != nil {
			return e
		}
		if s.OriginalConnected {
			for i := 0; i < 5; i++ {
				_ = hotspotNative("wfmConnectWirelessSilently")
				time.Sleep(2 * time.Second)
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				status, _ := exec.CommandContext(ctx, "/bin/wpa_cli", "-i", "wlan0", "status").Output()
				cancel()
				if _, err := wifiIP(); err == nil && strings.Contains(string(status), "wpa_state=COMPLETED") {
					success = true
					return nil
				}
			}
			log.Print("Normal Wi-Fi restored; saved network unavailable. Connect using Kobo Wi-Fi settings.")
		}
	}
	success = true
	return nil
}

func unloadHotspotRadio() error {
	for _, name := range []string{"8723ds", "sdio_wifi_pwr"} {
		if _, err := os.Stat("/sys/module/" + name); os.IsNotExist(err) {
			continue
		}
		var err error
		for i := 0; i < 4; i++ {
			err = hotspotCommand("/sbin/rmmod", name)
			if err == nil {
				break
			}
			time.Sleep(250 * time.Millisecond)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func hotspotGuard(pidText, generation string) error {
	pid, err := strconv.Atoi(pidText)
	if err != nil || pid <= 1 || generation == "" {
		return fmt.Errorf("invalid hotspot guard process identity")
	}
	expected := hotspotProcess{PID: pid, Generation: generation}
	for {
		s, e := readHotspotState()
		if os.IsNotExist(e) {
			return nil
		}
		if e != nil {
			return e
		}
		if s.Parent != expected {
			return nil
		}
		if !s.Parent.alive() || time.Now().After(s.Deadline) {
			// Stop a stuck receiver first, so it cannot outlive network restoration.
			if s.Parent.alive() {
				p, _ := os.FindProcess(s.Parent.PID)
				_ = p.Signal(os.Interrupt)
				time.Sleep(10 * time.Second)
				if s.Parent.alive() {
					_ = p.Kill()
				}
			}
			if e = restoreHotspot(expected); e != nil {
				log.Printf("Hotspot guard: %v", e)
			}
		}
		time.Sleep(2 * time.Second)
	}
}
