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

	script := fmt.Sprintf(`
from pyicloud import PyiCloudService
api = PyiCloudService('%s')
for i, d in enumerate(api.devices):
    print(f'  device {i}: {d}')
print(f'  iphone shortcut: {api.iphone}')
api.iphone.play_sound()
`, email)

	out, err := exec.Command("python3", "-c", script).CombinedOutput()
	output := strings.TrimSpace(string(out))
	if err != nil {
		fmt.Printf("  iPhone ping error: %s (%s)\n", err, output)
		return
	}
	fmt.Printf("  %s\n  iPhone → ping sent\n", output)
}
