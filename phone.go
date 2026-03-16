package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func pingIPhone() {
	email := os.Getenv("ICLOUD_EMAIL")
	if email == "" {
		fmt.Println("  iPhone ping: ICLOUD_EMAIL not set")
		return
	}

	cmd := fmt.Sprintf(
		"from pyicloud import PyiCloudService; api = PyiCloudService('%s'); api.iphone.play_sound()",
		email,
	)
	out, err := exec.Command("python3", "-c", cmd).CombinedOutput()
	output := strings.TrimSpace(string(out))
	if err != nil {
		fmt.Printf("  iPhone ping error: %s (%s)\n", err, output)
		return
	}
	fmt.Println("  iPhone → ping sent")
}
