package main

import (
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const (
	tvADBAddr = "192.168.1.175:5555"
	padTV     = 51
)

func isTVOn() bool {
	out, err := exec.Command("adb", "-s", tvADBAddr, "shell", "dumpsys", "power").CombinedOutput()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "Display Power: state=ON")
}

func tvPadColor(on bool) uint8 {
	if on {
		return colorOn
	}
	return colorNotOn
}

func pollTVStatus(stop <-chan struct{}) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			setPadColor(padTV, tvPadColor(isTVOn()))
		}
	}
}

func toggleTVMute() {
	out, err := exec.Command("adb", "-s", tvADBAddr, "shell", "input", "keyevent", "164").CombinedOutput()
	if err != nil {
		fmt.Printf("  TV mute error: %s (%s)\n", err, strings.TrimSpace(string(out)))
		return
	}
	fmt.Println("  TV → mute toggled")
}

func toggleTV() {
	setPadPulse(padTV, colorPulseLoad)

	wasOn := isTVOn()
	out, err := exec.Command("adb", "-s", tvADBAddr, "shell", "input", "keyevent", "26").CombinedOutput()
	if err != nil {
		fmt.Printf("  TV power error: %s (%s)\n", err, strings.TrimSpace(string(out)))
		setPadColor(padTV, tvPadColor(isTVOn()))
		return
	}

	time.Sleep(2 * time.Second)
	on := isTVOn()
	setPadColor(padTV, tvPadColor(on))

	state := "OFF"
	if on {
		state = "ON"
	}
	fmt.Printf("  TV → %s (was %v)\n", state, wasOn)
}
