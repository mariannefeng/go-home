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

func isSpeakerConnected() bool {
	out, err := exec.Command("bluetoothctl", "info", speakerMAC).CombinedOutput()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "Connected: yes")
}

func speakerPadColor(connected bool) uint8 {
	if connected {
		return colorGreen
	}
	return colorRed
}

func pollSpeakerStatus(stop <-chan struct{}) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			connected := isSpeakerConnected()
			setPadColor(padSpeaker, speakerPadColor(connected))
		}
	}
}

func toggleSpeaker() {
	if isSpeakerConnected() {
		fmt.Printf("  %s: disconnecting...\n", speakerName)
		out, err := exec.Command("bluetoothctl", "disconnect", speakerMAC).CombinedOutput()
		if err != nil {
			fmt.Printf("  disconnect error: %s (%s)\n", err, strings.TrimSpace(string(out)))
			return
		}
		fmt.Printf("  %s → disconnected\n", speakerName)
		setPadColor(padSpeaker, colorRed)
	} else {
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
	}
}
