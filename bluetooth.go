package main

import (
	"fmt"
	"os/exec"
	"strings"
)

const (
	SpeakerMAC  = "2C:41:A1:6D:9A:FE"
	SpeakerName = "Deep Space Fine"
)

func bluetoothIsConnected() bool {
	return bluetoothStatus() == StatusGood
}

func bluetoothStatus() DeviceStatus {
	out, err := exec.Command("bluetoothctl", "info", SpeakerMAC).CombinedOutput()
	if err != nil {
		return StatusIndeterminate
	}
	if strings.Contains(string(out), "Connected: yes") {
		return StatusGood
	}
	return StatusBad
}

func bluetoothToggle() (connected bool, err error) {
	if bluetoothIsConnected() {
		fmt.Printf("  %s: disconnecting...\n", SpeakerName)
		out, err := exec.Command("bluetoothctl", "disconnect", SpeakerMAC).CombinedOutput()
		if err != nil {
			return bluetoothIsConnected(), fmt.Errorf("disconnect: %w (%s)", err, strings.TrimSpace(string(out)))
		}
		fmt.Printf("  %s → disconnected\n", SpeakerName)
		return false, nil
	}
	fmt.Printf("  %s: connecting...\n", SpeakerName)
	out, err := exec.Command("bluetoothctl", "connect", SpeakerMAC).CombinedOutput()
	if err != nil {
		return bluetoothIsConnected(), fmt.Errorf("connect: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	if strings.Contains(string(out), "Connection successful") {
		fmt.Printf("  %s → connected\n", SpeakerName)
		return true, nil
	}
	fmt.Printf("  connect result: %s\n", strings.TrimSpace(string(out)))
	return bluetoothIsConnected(), nil
}
