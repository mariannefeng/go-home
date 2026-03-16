package main

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
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
