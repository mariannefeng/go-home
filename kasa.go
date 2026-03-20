package main

import (
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
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

	LIVING_ROOM   = "nommy cam"
	LIVING_ROOMIP = "192.168.1.156"
	OFFICE        = "awwfice"
	OFFICEIP      = "192.168.1.151"

	KasaCamHTTPSPort = 10443
	KasaCamHTTPSPath = "/data/LINKIE.json"
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
	kasaInitBulbs()
	kasaInitCameras()
}

func kasaInitBulbs() {
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
	fmt.Println("Connecting to Kasa cameras...")

	knownCameras := []struct {
		fallbackAlias string
		ip            string
	}{
		{fallbackAlias: LIVING_ROOM, ip: LIVING_ROOMIP},
		{fallbackAlias: OFFICE, ip: OFFICEIP},
	}

	camerasMu.Lock()
	cameras = make([]cameraState, len(knownCameras))
	camerasMu.Unlock()

	for i, c := range knownCameras {
		ip := c.ip
		si, on, err := queryCameraSysinfo(ip)
		alias := ""
		devName := ""
		model := ""
		if si != nil {
			alias = si.Alias
			devName = si.DevName
			model = si.Model
		}
		if alias == "" {
			alias = c.fallbackAlias
		}

		if err != nil {
			fmt.Printf("  camera[%d] alias=%q ip=%s query error: %s (on=false)\n", i+1, alias, ip, err)
			on = false
		}

		camerasMu.Lock()
		cameras[i] = cameraState{
			info: cameraInfo{
				alias:   alias,
				devName: devName,
				model:   model,
				ip:      ip,
			},
			on: on,
		}
		camerasMu.Unlock()
	}

	for i := range cameras {
		fmt.Printf("alias=%q ip=%s model=%s on=%t\n",
			cameras[i].info.alias, cameras[i].info.ip, cameras[i].info.model, cameras[i].on)
	}
	fmt.Println()
}

func kasaGetCameraStates() map[string]bool {
	camerasMu.Lock()
	defer camerasMu.Unlock()

	out := make(map[string]bool, len(cameras))
	for i := range cameras {
		out[cameras[i].info.alias] = cameras[i].on
	}
	return out
}

func kasaRefreshCameras() map[string]bool {
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

func kasaToggleCamera(alias string) (on bool, err error) {
	camerasMu.Lock()
	idx := -1
	for i := range cameras {
		// Prefer matching by Kasa app alias (what we print in init).
		if cameras[i].info.alias == alias {
			idx = i
			break
		}
	}
	if idx < 0 || idx >= len(cameras) {
		camerasMu.Unlock()
		return false, fmt.Errorf("camera alias %q not found (known: %v)", alias, cameraAliases())
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
	fmt.Printf("  CAMERA %s (%s) → %s\n", current.info.alias, current.info.ip, state)
	return verifiedOn, nil
}

func cameraAliases() []string {
	camerasMu.Lock()
	defer camerasMu.Unlock()

	out := make([]string, 0, len(cameras))
	for i := range cameras {
		out = append(out, cameras[i].info.alias)
	}
	return out
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

	// Camera "switch" enable/disable payload.
	//
	// Intended to mirror the python-kasa proto shape:
	//   smartlife.cam.ipcamera.switch -> set_is_enable -> (system.get_sysinfo.camera_switch = x["value"])
	// cmdSetCameraEnableOn  = `{"smartlife.cam.ipcamera.switch":{"set_is_enable":{"system":{"get_sysinfo":{"camera_switch":"on"}}}}}`
	cmdSetCameraEnableOn = `{"smartlife.cam.ipcamera.switch":{"set_is_enable":{"value":"on"}}}`
	// cmdSetCameraEnableOff = `{"smartlife.cam.ipcamera.switch":{"set_is_enable":{"system":{"get_sysinfo":{"camera_switch":"off"}}}}}`
	cmdSetCameraEnableOff = `{"smartlife.cam.ipcamera.switch":{"set_is_enable":{"value":"off"}}}`
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
	si, on, err := queryCameraSysinfo(ip)
	_ = si
	return on, err
}

type cameraSysinfoResponse struct {
	System struct {
		GetSysinfo struct {
			System cameraSysinfo `json:"system"`
		} `json:"get_sysinfo"`
	} `json:"system"`
}

type cameraSysinfo struct {
	Alias        string `json:"alias"`
	DevName      string `json:"dev_name"`
	Model        string `json:"model"`
	CameraSwitch string `json:"camera_switch"`
	LedStatus    string `json:"led_status"`
}

func queryCameraSysinfo(ip string) (*kasa.Sysinfo, bool, error) {
	// Kasa “relay-like” devices often respond to the UDP protocol (same as discovery).
	raddr, err := net.ResolveUDPAddr("udp4", fmt.Sprintf("%s:9999", ip))
	if err != nil {
		return nil, false, err
	}

	conn, err := net.DialUDP("udp4", nil, raddr)
	if err != nil {
		return nil, false, err
	}
	defer conn.Close()

	if err := conn.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		return nil, false, err
	}

	cmd := kasa.Scramble(kasa.CmdGetSysinfo)
	if _, err := conn.Write(cmd); err != nil {
		return nil, false, err
	}

	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		return nil, false, err
	}

	raw := kasa.Unscramble(buf[:n])

	var resp cameraSysinfoResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, false, fmt.Errorf("unmarshal camera sysinfo: %w (raw: %s)", err, string(raw))
	}

	sys := resp.System.GetSysinfo.System

	// The EC70 uses `camera_switch` ("on"/"off") to represent power.
	on := sys.CameraSwitch == "on"

	fmt.Printf("  camera sysinfo parsed ip=%s camera_switch=%q alias=%q dev_name=%q model=%q led_status=%q\n",
		ip,
		sys.CameraSwitch,
		sys.Alias,
		sys.DevName,
		sys.Model,
		sys.LedStatus,
	)

	// We return a kasa.Sysinfo for compatibility with callers.
	// Not all fields will be populated for these cameras.
	return &kasa.Sysinfo{
		Alias:   sys.Alias,
		DevName: sys.DevName,
		Model:   sys.Model,
	}, on, nil
}

func setCameraOn(ip string, on bool) error {
	// Mirror the python-kasa shape you specified:
	//   smartlife.cam.ipcamera.switch -> set_is_enable -> system.get_sysinfo.camera_switch = x["value"]
	cmd := cmdSetCameraEnableOff
	if on {
		cmd = cmdSetCameraEnableOn
	}

	fmt.Printf("  trying camera cmd to ip=%s cmd=%s\n", ip, cmd)
	if err := sendKasaCamHTTPSCommand(ip, cmd); err != nil {
		return err
	}

	time.Sleep(300 * time.Millisecond)
	currentOn, qerr := queryCameraOn(ip)
	if qerr == nil && currentOn == on {
		fmt.Printf("  camera cmd succeeded ip=%s => camera_switch=%t\n", ip, currentOn)
		return nil
	}
	if qerr != nil {
		return qerr
	}
	return fmt.Errorf("verification mismatch after camera cmd (got on=%t desired on=%t)", currentOn, on)
}

func sendKasaCamHTTPSCommand(ip, plaintextRequest string) error {
	// python-kasa PR #537 does:
	// - encrypted_cmd = SmartCameraProtocol.encrypt(request)[4:]
	// - b64_cmd = base64(encrypted_cmd)
	// - url_safe_cmd = quote(b64_cmd)
	// - POST /data/LINKIE.json with form body "content=<url_safe_cmd>"
	//
	// go-kasa's Scramble() matches the "encrypt()[4:]" behavior (XOR bytes without length prefix).
	encryptedCmd := kasa.Scramble(plaintextRequest)
	b64Cmd := base64.StdEncoding.EncodeToString(encryptedCmd)
	urlSafeCmd := url.QueryEscape(b64Cmd)

	form := "content=" + urlSafeCmd

	httpsURL := fmt.Sprintf("https://%s:%d%s", ip, KasaCamHTTPSPort, KasaCamHTTPSPath)

	// http.Client transport with disabled TLS verification (camera uses self-signed certs).
	tr := &http.Transport{
		ForceAttemptHTTP2: false,
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true, //nolint:gosec
			// Some Kasa Cam firmwares don't support newer TLS negotiation (e.g. TLS 1.3),
			// so we explicitly limit protocol versions and ALPN.
			MinVersion: tls.VersionTLS12,
			MaxVersion: tls.VersionTLS12,
			// NextProtos: []string{"http/1.1"},
			CipherSuites: []uint16{
				tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
				tls.TLS_RSA_WITH_AES_256_GCM_SHA384,
			},
		},
	}
	client := &http.Client{
		Timeout:   5 * time.Second,
		Transport: tr,
	}

	req, err := http.NewRequest(http.MethodPost, httpsURL, strings.NewReader(form))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	// Optional BasicAuth (python-kasa uses base64(username/password) style for the password).
	// We only send this if both vars are set.
	if user := os.Getenv("KASA_USERNAME"); user != "" {
		pass := os.Getenv("KASA_PASSWORD")
		if pass != "" {
			fmt.Println("  BasicAuthhhhhhh: username=", user, "password=", pass)
			b64Pass := base64.StdEncoding.EncodeToString([]byte(pass))
			req.SetBasicAuth(user, b64Pass)
		}
	}

	resp, err := client.Do(req)

	fmt.Printf("sendKasaCamHTTPSCommand request: POST %s\n", httpsURL)
	fmt.Printf("  Content-Type: application/x-www-form-urlencoded\n")
	fmt.Printf("  Body: %s\n", form)

	if resp != nil {
		fmt.Printf("sendKasaCamHTTPSCommand response: %s\n", resp.Status)
		bodyBytes, _ := io.ReadAll(resp.Body)
		raw := strings.TrimSpace(string(bodyBytes))
		fmt.Printf("  Body (raw): %s\n", raw)

		// Camera responses are XOR-encrypted bytes base64-encoded.
		// Decode+unscramble so logs are readable.
		if enc, err := base64.StdEncoding.DecodeString(raw); err != nil {
			fmt.Printf("  Body (decode error): %v\n", err)
		} else {
			plain := kasa.Unscramble(enc)
			fmt.Printf("  Body (decrypted): %s\n", string(plain))

			// Best-effort JSON parse for clearer logging.
			var parsed any
			if err := json.Unmarshal(plain, &parsed); err == nil {
				fmt.Printf("  Body (decrypted JSON): %+v\n", parsed)
			}
		}
	}

	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Drain body to allow connection reuse.
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("camera https post failed: status=%s", resp.Status)
	}

	return nil
}

func sendKasaUDPCommand(ip, cmd string) error {
	raddr, err := net.ResolveUDPAddr("udp4", fmt.Sprintf("%s:9999", ip))
	if err != nil {
		return err
	}

	conn, err := net.DialUDP("udp4", nil, raddr)
	if err != nil {
		return err
	}
	defer conn.Close()

	if err := conn.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
		return err
	}

	_, err = conn.Write(kasa.Scramble(cmd))
	return err
}
