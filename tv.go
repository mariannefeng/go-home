package main

import (
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const (
	ADBAddr = "192.168.1.175:5555"
)

func tvInit() {
	out, err := exec.Command("adb", "connect", ADBAddr).CombinedOutput()
	s := strings.TrimSpace(string(out))
	if err != nil {
		fmt.Printf("TV: adb connect: %v (%s)\n", err, s)
		return
	}
	if s != "" {
		fmt.Printf("TV: %s\n", s)
	}
}

func tvIsOn() bool {
	out, err := exec.Command("adb", "-s", ADBAddr, "shell", "dumpsys", "power").CombinedOutput()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "Display Power: state=ON")
}

func tvToggleMute() error {
	out, err := exec.Command("adb", "-s", ADBAddr, "shell", "input", "keyevent", "164").CombinedOutput()
	if err != nil {
		return fmt.Errorf("mute: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	fmt.Println("  TV → mute toggled")
	return nil
}

func tvToggle() (on bool, err error) {
	wasOn := tvIsOn()
	out, err := exec.Command("adb", "-s", ADBAddr, "shell", "input", "keyevent", "26").CombinedOutput()
	if err != nil {
		return tvIsOn(), fmt.Errorf("power: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	time.Sleep(2 * time.Second)
	on = tvIsOn()
	state := "OFF"
	if on {
		state = "ON"
	}
	fmt.Printf("  TV → %s (was %v)\n", state, wasOn)
	return on, nil
}

func tvIsConnected() bool {
	out, err := exec.Command("adb", "-s", ADBAddr, "shell", "echo", "connected").CombinedOutput()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "connected")
}