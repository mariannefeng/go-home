package main

import (
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const (
	speakerMAC  = "2C:41:A1:6D:9A:FE"
	speakerName = "Deep Space Fine"

	padSpeaker = 47
	colorBlue  = 45
)

type btStatus int

const (
	btOff        btStatus = iota // unreachable / not paired
	btOn                         // paired but not connected
	btConnected                  // paired and connected
)

func checkSpeakerStatus() btStatus {
	out, err := exec.Command("bluetoothctl", "info", speakerMAC).CombinedOutput()
	if err != nil {
		return btOff
	}

	info := string(out)
	if !strings.Contains(info, "Paired: yes") {
		return btOff
	}
	if strings.Contains(info, "Connected: yes") {
		return btConnected
	}
	return btOn
}

func speakerPadColor(status btStatus) uint8 {
	switch status {
	case btConnected:
		return colorGreen
	case btOn:
		return colorBlue
	default:
		return colorRed
	}
}

func pollSpeakerStatus(stop <-chan struct{}) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	var last btStatus = -1
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			status := checkSpeakerStatus()
			if status != last {
				fmt.Printf("Speaker status changed → %s\n", []string{"off", "on", "connected"}[status])
				setPadColor(padSpeaker, speakerPadColor(status))
				last = status
			}
		}
	}
}

func toggleSpeaker() {
	status := checkSpeakerStatus()

	switch status {
	case btConnected:
		fmt.Printf("  %s: disconnecting...\n", speakerName)
		out, err := exec.Command("bluetoothctl", "disconnect", speakerMAC).CombinedOutput()
		if err != nil {
			fmt.Printf("  disconnect error: %s (%s)\n", err, strings.TrimSpace(string(out)))
			return
		}
		fmt.Printf("  %s → disconnected\n", speakerName)
		setPadColor(padSpeaker, colorBlue)

	case btOn:
		fmt.Printf("  %s: connecting...\n", speakerName)
		out, err := exec.Command("bluetoothctl", "connect", speakerMAC).CombinedOutput()
		if err != nil {
			fmt.Printf("  connect error: %s (%s)\n", err, strings.TrimSpace(string(out)))
			return
		}
		if strings.Contains(string(out), "Connection successful") {
			fmt.Printf("  %s → connected\n", speakerName)
			setPadColor(padSpeaker, colorGreen)
		} else {
			fmt.Printf("  connect result: %s\n", strings.TrimSpace(string(out)))
		}

	default:
		fmt.Printf("  %s: speaker is off or not paired\n", speakerName)
	}
}
