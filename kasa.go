package main

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cloudkucooland/go-kasa"
)

const (
	MushroomLamp   = "Mushroom bulb"
	MushroomLampIP = "192.168.1.160"
	FlowerLamp     = "Flower lamp"
	FlowerLampIP   = "192.168.1.153"
)

var knownBulbs = []struct {
	alias string
	ip    string
}{
	{FlowerLamp, FlowerLampIP},
	{MushroomLamp, MushroomLampIP},
}

var (
	bulbs map[string]bulbState
	mu    sync.Mutex
)

type cameraInfo struct {
	alias   string
	devName string
	model   string
	ip      string
}

type cameraState struct {
	info cameraInfo
	on   bool
}

var (
	cameras   []cameraState
	camerasMu sync.Mutex
)

type bulbState struct {
	alias      string
	ip         string
	on         bool
	brightness uint8
}

func kasaInit() {
	fmt.Println("Connecting to Kasa bulbs...")
	bulbs = make(map[string]bulbState)

	for _, kb := range knownBulbs {
		ls, err := queryLightState(kb.ip)
		if err != nil {
			fmt.Printf("  %s (%s): error: %s\n", kb.alias, kb.ip, err)
			continue
		}

		on := ls.OnOff == 1
		state := "OFF"
		if on {
			state = "ON"
		}
		fmt.Printf("  %s (%s): %s  brightness=%d%%\n", kb.alias, kb.ip, state, ls.Brightness)

		bulbs[kb.alias] = bulbState{
			alias:      kb.alias,
			ip:         kb.ip,
			on:         on,
			brightness: ls.Brightness,
		}
	}
	fmt.Println()

	kasaInitCameras()
}

func kasaGetBulbStates() map[string]bool {
	mu.Lock()
	defer mu.Unlock()
	result := make(map[string]bool)
	for alias, b := range bulbs {
		result[alias] = b.on
	}
	return result
}

func kasaRefresh() map[string]bool {
	for _, kb := range knownBulbs {
		ls, err := queryLightState(kb.ip)
		if err != nil {
			continue
		}
		on := ls.OnOff == 1
		mu.Lock()
		b := bulbs[kb.alias]
		b.alias = kb.alias
		b.ip = kb.ip
		b.on = on
		b.brightness = ls.Brightness
		bulbs[kb.alias] = b
		mu.Unlock()
	}
	return kasaGetBulbStates()
}

func kasaToggleLamp(alias string) (on bool, err error) {
	mu.Lock()
	b, ok := bulbs[alias]
	if !ok {
		mu.Unlock()
		kasaRefresh()
		mu.Lock()
		b, ok = bulbs[alias]
		if !ok {
			mu.Unlock()
			return false, fmt.Errorf("lamp %q unreachable", alias)
		}
	}

	newState := !b.on
	if err := setLightState(b.ip, newState); err != nil {
		return b.on, err
	}

	b.on = newState
	bulbs[alias] = b
	mu.Unlock()

	state := "OFF"
	if newState {
		state = "ON"
	}
	fmt.Printf("  %s → %s\n", alias, state)
	return newState, nil
}

func kasaInitCameras() {
	fmt.Println("Discovering Kasa cameras...")

	// BroadcastDiscovery blocks until its internal listener timeout fires.
	// Keep this small since we only need 2 devices.
	devices, err := kasa.BroadcastDiscovery(2, 2)
	if err != nil {
		fmt.Printf("  camera discovery error: %s\n\n", err)
		return
	}

	type discovered struct {
		ip string
		si *kasa.Sysinfo
	}
	all := make([]discovered, 0, len(devices))
	for ip, si := range devices {
		all = append(all, discovered{ip: ip, si: si})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].ip < all[j].ip })

	fmt.Printf("  discovered %d device(s):\n", len(all))
	for _, d := range all {
		on := d.si.RelayState == 1
		fmt.Printf("    - %s (%s) model=%s ip=%s relay_state=%d (on=%t)\n",
			d.si.Alias, d.si.DevName, d.si.Model, d.ip, d.si.RelayState, on)
	}

	looksLikeCamera := func(si *kasa.Sysinfo) bool {
		// Camera models are typically KCxxx (TP-Link Kasa cameras).
		model := strings.ToUpper(si.Model)
		alias := strings.ToUpper(si.Alias)
		devName := strings.ToUpper(si.DevName)
		feature := strings.ToUpper(si.Feature)

		return strings.Contains(model, "KC") ||
			strings.Contains(alias, "CAM") ||
			strings.Contains(devName, "CAM") ||
			strings.Contains(feature, "CAM")
	}

	candidates := make([]cameraInfo, 0, 2)
	for _, d := range all {
		if looksLikeCamera(d.si) {
			candidates = append(candidates, cameraInfo{
				alias:   d.si.Alias,
				devName: d.si.DevName,
				model:   d.si.Model,
				ip:      d.ip,
			})
		}
	}

	// If we can't confidently identify two cameras, fall back to first two discovered
	// devices so we can still wire/ping the expected “on/off” behavior.
	if len(candidates) < 2 && len(all) >= 2 {
		fmt.Println("  couldn't identify 2 cameras via model/name; falling back to first two discovered devices")
		candidates = candidates[:0]
		for i := 0; i < 2 && i < len(all); i++ {
			candidates = append(candidates, cameraInfo{
				alias:   all[i].si.Alias,
				devName: all[i].si.DevName,
				model:   all[i].si.Model,
				ip:      all[i].ip,
			})
		}
	}

	if len(candidates) == 0 {
		fmt.Println()
		fmt.Println("  no camera candidates found (camera pads will be inactive)")
		return
	}
	if len(candidates) < 2 {
		fmt.Printf("  found %d camera candidate(s); camera pad for missing device will be inactive\n", len(candidates))
	}

	camerasMu.Lock()
	cameras = make([]cameraState, len(candidates))
	camerasMu.Unlock()

	for i := range candidates {
		on, err := queryCameraOn(candidates[i].ip)
		if err != nil {
			fmt.Printf("  camera[%d] (%s) ip=%s query error: %s; using relay_state from discovery\n", i+1, candidates[i].alias, candidates[i].ip, err)
			// Best-effort: use relay_state from discovery if available.
			// (If missing, default to false.)
			on = false
			for _, d := range all {
				if d.ip == candidates[i].ip {
					on = d.si.RelayState == 1
					break
				}
			}
		}

		camerasMu.Lock()
		cameras[i] = cameraState{info: candidates[i], on: on}
		camerasMu.Unlock()
	}

	for i := range cameras {
		fmt.Printf("  CAMERA %d: alias=%q ip=%s model=%s on=%t\n",
			i+1, cameras[i].info.alias, cameras[i].info.ip, cameras[i].info.model, cameras[i].on)
	}
	fmt.Println()
}

func kasaGetCameraStates() []bool {
	camerasMu.Lock()
	defer camerasMu.Unlock()

	out := make([]bool, len(cameras))
	for i := range cameras {
		out[i] = cameras[i].on
	}
	return out
}

func kasaRefreshCameras() []bool {
	camerasMu.Lock()
	n := len(cameras)
	ips := make([]string, 0, n)
	for i := 0; i < n; i++ {
		ips = append(ips, cameras[i].info.ip)
	}
	camerasMu.Unlock()

	for i := 0; i < n; i++ {
		on, err := queryCameraOn(ips[i])
		if err != nil {
			continue
		}

		camerasMu.Lock()
		cameras[i].on = on
		camerasMu.Unlock()
	}

	return kasaGetCameraStates()
}

func kasaToggleCamera(idx int) (on bool, err error) {
	camerasMu.Lock()
	if idx < 0 || idx >= len(cameras) {
		camerasMu.Unlock()
		return false, fmt.Errorf("camera idx %d out of range", idx)
	}
	current := cameras[idx]
	camerasMu.Unlock()

	target := !current.on
	if err := setCameraOn(current.info.ip, target); err != nil {
		return current.on, err
	}

	// Verify by querying state, since some devices may accept the TCP write
	// but not actually change state.
	time.Sleep(200 * time.Millisecond)
	verifiedOn, qerr := queryCameraOn(current.info.ip)
	if qerr != nil {
		verifiedOn = target
	}

	camerasMu.Lock()
	// idx might still be valid; if cameras were re-discovered concurrently (rare),
	// we keep the last-known state.
	if idx >= 0 && idx < len(cameras) {
		cameras[idx].on = verifiedOn
	}
	camerasMu.Unlock()

	state := "OFF"
	if verifiedOn {
		state = "ON"
	}
	fmt.Printf("  CAMERA[%d] %s (%s) → %s\n", idx+1, current.info.alias, current.info.ip, state)
	return verifiedOn, nil
}

var lastBrightnessSend sync.Map

func kasaSetBrightness(alias string, brightness int) (on bool, err error) {
	mu.Lock()
	b, ok := bulbs[alias]
	mu.Unlock()
	if !ok {
		return false, nil
	}

	if last, ok := lastBrightnessSend.Load(alias); ok {
		if time.Since(last.(time.Time)) < 100*time.Millisecond {
			return b.on, nil
		}
	}
	lastBrightnessSend.Store(alias, time.Now())

	if err := setLightBrightness(b.ip, brightness); err != nil {
		return b.on, err
	}

	newOn := brightness > 0

	mu.Lock()
	b.brightness = uint8(brightness)
	b.on = newOn
	bulbs[alias] = b
	mu.Unlock()

	return newOn, nil
}

// --- Raw Kasa TCP protocol ---

type lightState struct {
	OnOff      uint8 `json:"on_off"`
	Brightness uint8 `json:"brightness"`
	Hue        int   `json:"hue"`
	Saturation int   `json:"saturation"`
	ColorTemp  int   `json:"color_temp"`
}

type lightStateResponse struct {
	LightingService struct {
		LightState lightState `json:"get_light_state"`
	} `json:"smartlife.iot.smartbulb.lightingservice"`
}

const (
	cmdGetLightState = `{"smartlife.iot.smartbulb.lightingservice":{"get_light_state":{}}}`
	cmdSetLightOn    = `{"smartlife.iot.smartbulb.lightingservice":{"transition_light_state":{"on_off":1}}}`
	cmdSetLightOff   = `{"smartlife.iot.smartbulb.lightingservice":{"transition_light_state":{"on_off":0}}}`
)

func queryLightState(ip string) (*lightState, error) {
	dialer := &net.Dialer{
		Timeout:  2 * time.Second,
		Deadline: time.Now().Add(3 * time.Second),
	}

	conn, err := dialer.Dial("tcp4", fmt.Sprintf("%s:9999", ip))
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	if _, err = conn.Write(kasa.ScrambleTCP(cmdGetLightState)); err != nil {
		return nil, err
	}

	header := make([]byte, 4)
	if _, err = conn.Read(header); err != nil {
		return nil, err
	}
	size := binary.BigEndian.Uint32(header)

	data := make([]byte, size)
	total := 0
	for total < int(size) {
		n, err := conn.Read(data[total:])
		if err != nil {
			return nil, err
		}
		total += n
	}

	raw := kasa.Unscramble(data)
	var resp lightStateResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal light state: %w (raw: %s)", err, string(raw))
	}

	return &resp.LightingService.LightState, nil
}

func setLightState(ip string, on bool) error {
	cmd := cmdSetLightOff
	if on {
		cmd = cmdSetLightOn
	}
	return sendKasaCommand(ip, cmd)
}

func setLightBrightness(ip string, brightness int) error {
	if brightness < 1 {
		return setLightState(ip, false)
	}
	if brightness > 100 {
		brightness = 100
	}
	cmd := fmt.Sprintf(
		`{"smartlife.iot.smartbulb.lightingservice":{"transition_light_state":{"on_off":1,"brightness":%d}}}`,
		brightness,
	)
	return sendKasaCommand(ip, cmd)
}

func sendKasaCommand(ip, cmd string) error {
	conn, err := net.DialTimeout("tcp4", fmt.Sprintf("%s:9999", ip), 2*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()

	_, err = conn.Write(kasa.ScrambleTCP(cmd))
	return err
}

func queryCameraOn(ip string) (bool, error) {
	// Many Kasa “relay-like” devices expose `relay_state` via get_sysinfo.
	dialer := &net.Dialer{
		Timeout:  2 * time.Second,
		Deadline: time.Now().Add(3 * time.Second),
	}

	conn, err := dialer.Dial("tcp4", fmt.Sprintf("%s:9999", ip))
	if err != nil {
		return false, err
	}
	defer conn.Close()

	if _, err = conn.Write(kasa.ScrambleTCP(kasa.CmdGetSysinfo)); err != nil {
		return false, err
	}

	header := make([]byte, 4)
	if _, err = conn.Read(header); err != nil {
		return false, err
	}
	size := binary.BigEndian.Uint32(header)

	data := make([]byte, size)
	total := 0
	for total < int(size) {
		n, err := conn.Read(data[total:])
		if err != nil {
			return false, err
		}
		total += n
	}

	raw := kasa.Unscramble(data)
	var kd kasa.KasaDevice
	if err := json.Unmarshal(raw, &kd); err != nil {
		return false, fmt.Errorf("unmarshal camera sysinfo: %w (raw: %s)", err, string(raw))
	}
	return kd.GetSysinfo.Sysinfo.RelayState == 1, nil
}

func setCameraOn(ip string, on bool) error {
	state := 0
	if on {
		state = 1
	}
	cmd := fmt.Sprintf(kasa.CmdSetRelayState, state)
	return sendKasaCommand(ip, cmd)
}
