package main

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"time"

	kasa "github.com/cloudkucooland/go-kasa"
)

const (
	MUSHROOM_LAMP    = "Mushroom bulb"
	MUSHROOM_LAMP_IP = "192.168.1.160"
	FLOWER_LAMP      = "Flower lamp"
	FLOWER_LAMP_IP   = "192.168.1.153"
)

var knownBulbs = []struct {
	alias string
	ip    string
}{
	{FLOWER_LAMP, FLOWER_LAMP_IP},
	{MUSHROOM_LAMP, MUSHROOM_LAMP_IP},
}

var (
	bulbs map[string]bulbState
	mu    sync.Mutex
)

type bulbState struct {
	alias      string
	ip         string
	on         bool
	brightness uint8
}

func initKasa() map[string]bulbState {
	fmt.Println("Connecting to Kasa bulbs...")
	result := make(map[string]bulbState)

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

		result[kb.alias] = bulbState{
			alias:      kb.alias,
			ip:         kb.ip,
			on:         on,
			brightness: ls.Brightness,
		}
	}
	fmt.Println()
	return result
}

func lampPadColor(on bool) uint8 {
	if on {
		return colorOn
	}
	return colorNotOn
}

func updateLampPads(bulbs map[string]bulbState) {
	if flower, ok := bulbs[FLOWER_LAMP]; ok {
		setPadColor(padFlowerLamp, lampPadColor(flower.on))
	} else {
		setPadColor(padFlowerLamp, colorRed)
	}

	if mushroom, ok := bulbs[MUSHROOM_LAMP]; ok {
		setPadColor(padMushroomLamp, lampPadColor(mushroom.on))
	} else {
		setPadColor(padMushroomLamp, colorRed)
	}
}

func pollLampStatus(stop <-chan struct{}) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
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
			mu.Lock()
			updateLampPads(bulbs)
			mu.Unlock()
		}
	}
}

func toggleLamp(alias string, pad uint8) {
	mu.Lock()
	defer mu.Unlock()

	b, ok := bulbs[alias]
	if !ok {
		fmt.Printf("lamp %q not in cache, querying directly...\n", alias)
		mu.Unlock()
		fresh := initKasa()
		mu.Lock()
		for k, v := range fresh {
			bulbs[k] = v
		}
		b, ok = bulbs[alias]
		if !ok {
			fmt.Printf("lamp %q still unreachable\n", alias)
			return
		}
		updateLampPads(bulbs)
	}

	newState := !b.on
	if err := setLightState(b.ip, newState); err != nil {
		fmt.Printf("error toggling %s: %s\n", alias, err)
		return
	}

	b.on = newState
	bulbs[alias] = b

	setPadColor(pad, lampPadColor(newState))

	state := "OFF"
	if newState {
		state = "ON"
	}
	fmt.Printf("  %s → %s\n", alias, state)
}

var lastBrightnessSend sync.Map

var lampPads = map[string]uint8{
	FLOWER_LAMP:   padFlowerLamp,
	MUSHROOM_LAMP: padMushroomLamp,
}

func setLampBrightness(alias string, brightness int) {
	mu.Lock()
	b, ok := bulbs[alias]
	mu.Unlock()
	if !ok {
		return
	}

	if last, ok := lastBrightnessSend.Load(alias); ok {
		if time.Since(last.(time.Time)) < 100*time.Millisecond {
			return
		}
	}
	lastBrightnessSend.Store(alias, time.Now())

	if err := setLightBrightness(b.ip, brightness); err != nil {
		fmt.Printf("error setting brightness for %s: %s\n", alias, err)
		return
	}

	newOn := brightness > 0
	wasOn := b.on

	mu.Lock()
	b.brightness = uint8(brightness)
	b.on = newOn
	bulbs[alias] = b
	mu.Unlock()

	if newOn != wasOn {
		if pad, ok := lampPads[alias]; ok {
			setPadColor(pad, lampPadColor(newOn))
		}
	}
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
