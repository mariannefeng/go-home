package main

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"time"

	kasa "github.com/cloudkucooland/go-kasa"
)

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

const cmdGetLightState = `{"smartlife.iot.smartbulb.lightingservice":{"get_light_state":{}}}`

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
