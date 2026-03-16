package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func pingIPhone() {
	email := os.Getenv("ICLOUD_EMAIL")
	device := os.Getenv("ICLOUD_DEVICE_NAME")
	if email == "" || device == "" {
		fmt.Println("  iPhone ping: ICLOUD_EMAIL or ICLOUD_DEVICE_NAME not set")
		return
	}

	script := fmt.Sprintf(`
from pyicloud import PyiCloudService
api = PyiCloudService('%s')
phone = next((d for d in api.devices if '%s' in str(d)), None)
if phone is None:
    raise Exception('No device matching "%s" found')
phone.play_sound()
print(f'pinged: {phone}')
`, email, device, device)

	out, err := exec.Command("python3", "-c", script).CombinedOutput()
	output := strings.TrimSpace(string(out))
	if err != nil {
		fmt.Printf("  iPhone ping error: %s (%s)\n", err, output)
		return
	}
	fmt.Printf("  %s\n", output)
}
