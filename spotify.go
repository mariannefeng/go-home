package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

var defaultPlaylists = []string{
	"spotify:playlist:37i9dQZF1E3a2W8bJ0xgR9", // Daily Mix 1
	"spotify:playlist:37i9dQZF1E36HHA342YoGB", // Daily Mix 2
	"spotify:playlist:37i9dQZF1E38KeS3y3DpMl", // Daily Mix 3
	"spotify:playlist:37i9dQZF1E39Vh93Of36Ww", // Daily Mix 4
	"spotify:playlist:37i9dQZF1E383AryV5uwV9", // Daily Mix 5
	"spotify:playlist:37i9dQZF1E37eOsAQc1AyM", // Daily Mix 6
}

type client struct {
	clientID     string
	clientSecret string
	refreshToken string
	deviceName   string

	mu          sync.Mutex
	accessToken string
	tokenExpiry time.Time
}

var c *client

func spotifyInit() {
	id := os.Getenv("SPOTIFY_CLIENT_ID")
	secret := os.Getenv("SPOTIFY_CLIENT_SECRET")
	refresh := os.Getenv("SPOTIFY_REFRESH_TOKEN")
	device := os.Getenv("SPOTIFY_DEVICE_NAME")

	if id == "" || secret == "" || refresh == "" {
		fmt.Println("Spotify: missing SPOTIFY_CLIENT_ID, SPOTIFY_CLIENT_SECRET, or SPOTIFY_REFRESH_TOKEN — disabled")
		return
	}

	c = &client{
		clientID:     id,
		clientSecret: secret,
		refreshToken: refresh,
		deviceName:   device,
	}

	if err := c.refresh(); err != nil {
		fmt.Printf("Spotify: initial token refresh failed: %s — disabled\n", err)
		c = nil
		return
	}
	fmt.Printf("Spotify: authenticated (device=%q)\n\n", device)
}

func (c *client) refresh() error {
	data := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {c.refreshToken},
	}

	req, err := http.NewRequest("POST", "https://accounts.spotify.com/api/token", strings.NewReader(data.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString(
		[]byte(c.clientID+":"+c.clientSecret),
	))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("token refresh HTTP %d: %s", resp.StatusCode, body)
	}

	var result struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return err
	}

	c.mu.Lock()
	c.accessToken = result.AccessToken
	c.tokenExpiry = time.Now().Add(time.Duration(result.ExpiresIn-60) * time.Second)
	c.mu.Unlock()

	return nil
}

func (c *client) token() (string, error) {
	c.mu.Lock()
	if time.Now().Before(c.tokenExpiry) {
		t := c.accessToken
		c.mu.Unlock()
		return t, nil
	}
	c.mu.Unlock()

	if err := c.refresh(); err != nil {
		return "", err
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	return c.accessToken, nil
}

func (c *client) apiRequest(method, path string, body io.Reader) (*http.Response, error) {
	tok, err := c.token()
	if err != nil {
		return nil, fmt.Errorf("auth: %w", err)
	}

	req, err := http.NewRequest(method, "https://api.spotify.com/v1"+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	return http.DefaultClient.Do(req)
}

// transferToDevice makes the given device the active one. If play is true, playback starts on that device.
func (c *client) transferToDevice(deviceID string, play bool) error {
	body := fmt.Sprintf(`{"device_ids":["%s"],"play":%t}`, deviceID, play)
	resp, err := c.apiRequest("PUT", "/me/player", strings.NewReader(body))
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != 204 {
		return fmt.Errorf("transfer playback HTTP %d", resp.StatusCode)
	}
	return nil
}

func (c *client) findDeviceID() (string, error) {
	resp, err := c.apiRequest("GET", "/me/player/devices", nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var result struct {
		Devices []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"devices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	for _, d := range result.Devices {
		if strings.EqualFold(d.Name, c.deviceName) {
			return d.ID, nil
		}
	}
	names := make([]string, len(result.Devices))
	for i, d := range result.Devices {
		names[i] = d.Name
	}
	return "", fmt.Errorf("device %q not found (available: %v)", c.deviceName, names)
}

// playbackState holds the result of GET /me/player. Zero value means no active playback (204 or error).
func (c *client) getPlaybackState() (deviceID, deviceName string, hasItem, isPlaying bool) {
	resp, err := c.apiRequest("GET", "/me/player", nil)
	if err != nil {
		return "", "", false, false
	}
	defer resp.Body.Close()

	if resp.StatusCode == 204 {
		return "", "", false, false
	}

	var state struct {
		Device *struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"device"`
		Item      *json.RawMessage `json:"item"`
		IsPlaying bool             `json:"is_playing"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&state); err != nil {
		return "", "", false, false
	}
	hasItem = state.Item != nil
	isPlaying = state.IsPlaying
	if state.Device != nil {
		deviceID = state.Device.ID
		deviceName = state.Device.Name
	}
	return deviceID, deviceName, hasItem, isPlaying
}

func spotifyPlay() {
	if c == nil {
		fmt.Println("Spotify: not configured")
		return
	}

	deviceID, _, hasItem, isPlaying := c.getPlaybackState()

	// Already playing somewhere — do nothing.
	if isPlaying {
		return
	}

	btConnected := bluetoothIsConnected()

	if !btConnected {
		// Bluetooth not connected: only resume on the existing device if there's paused playback.
		if hasItem && deviceID != "" {
			if err := c.transferToDevice(deviceID, true); err != nil {
				fmt.Printf("Spotify play error: %s\n", err)
				return
			}
			fmt.Println("  Spotify → playing (resumed on current device)")
		}
		return
	}

	// Bluetooth connected: play on sauron.
	sauronID, err := c.findDeviceID()
	if err != nil {
		fmt.Printf("Spotify play error: %s\n", err)
		return
	}

	if hasItem {
		// Paused playback exists — transfer to sauron and resume (one call makes sauron active + starts playback).
		transferBody := fmt.Sprintf(`{"device_ids":["%s"],"play":true}`, sauronID)
		resp, err := c.apiRequest("PUT", "/me/player", strings.NewReader(transferBody))
		if err != nil {
			fmt.Printf("Spotify play error: %s\n", err)
			return
		}
		resp.Body.Close()
		if resp.StatusCode == 204 {
			fmt.Println("  Spotify → playing on sauron (resumed)")
		} else {
			respBody, _ := io.ReadAll(resp.Body)
			fmt.Printf("  Spotify transfer/resume HTTP %d: %s\n", resp.StatusCode, respBody)
		}
		return
	}

	// No active playback — transfer to sauron then start a random Daily Mix.
	if err := c.transferToDevice(sauronID, false); err != nil {
		fmt.Printf("Spotify play error: %s\n", err)
		return
	}
	playlist := defaultPlaylists[rand.Intn(len(defaultPlaylists))]
	fmt.Printf("  Spotify: nothing active, starting %s\n", playlist)

	body := fmt.Sprintf(`{"context_uri":"%s"}`, playlist)
	resp, err := c.apiRequest("PUT", "/me/player/play?device_id="+sauronID, strings.NewReader(body))
	if err != nil {
		fmt.Printf("Spotify play error: %s\n", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == 204 || resp.StatusCode == 200 {
		fmt.Println("  Spotify → playing (random Daily Mix)")
	} else {
		respBody, _ := io.ReadAll(resp.Body)
		fmt.Printf("  Spotify play HTTP %d: %s\n", resp.StatusCode, respBody)
	}
}

func spotifyAdjustVolume(delta int) {
	if c == nil {
		fmt.Println("Spotify: not configured")
		return
	}

	resp, err := c.apiRequest("GET", "/me/player", nil)
	if err != nil {
		fmt.Printf("Spotify volume error: %s\n", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == 204 {
		fmt.Println("Spotify volume: no active playback")
		return
	}

	var state struct {
		Device struct {
			VolumePercent int `json:"volume_percent"`
		} `json:"device"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&state); err != nil {
		fmt.Printf("Spotify volume error: %s\n", err)
		return
	}

	vol := state.Device.VolumePercent + delta
	if vol > 100 {
		vol = 100
	}
	if vol < 0 {
		vol = 0
	}

	path := fmt.Sprintf("/me/player/volume?volume_percent=%d", vol)
	vResp, err := c.apiRequest("PUT", path, nil)
	if err != nil {
		fmt.Printf("Spotify volume error: %s\n", err)
		return
	}
	defer vResp.Body.Close()

	if vResp.StatusCode == 204 || vResp.StatusCode == 200 {
		fmt.Printf("  Spotify → volume %d%%\n", vol)
	} else {
		body, _ := io.ReadAll(vResp.Body)
		fmt.Printf("  Spotify volume HTTP %d: %s\n", vResp.StatusCode, body)
	}
}

func spotifyNext() {
	if c == nil {
		fmt.Println("Spotify: not configured")
		return
	}

	resp, err := c.apiRequest("POST", "/me/player/next", nil)
	if err != nil {
		fmt.Printf("Spotify next error: %s\n", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == 204 || resp.StatusCode == 200 {
		fmt.Println("  Spotify → next track")
	} else {
		body, _ := io.ReadAll(resp.Body)
		fmt.Printf("  Spotify next HTTP %d: %s\n", resp.StatusCode, body)
	}
}

func spotifyPrev() {
	if c == nil {
		fmt.Println("Spotify: not configured")
		return
	}

	resp, err := c.apiRequest("POST", "/me/player/previous", nil)
	if err != nil {
		fmt.Printf("Spotify prev error: %s\n", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == 204 || resp.StatusCode == 200 {
		fmt.Println("  Spotify → previous track")
	} else {
		body, _ := io.ReadAll(resp.Body)
		fmt.Printf("  Spotify prev HTTP %d: %s\n", resp.StatusCode, body)
	}
}

func spotifyPause() {
	if c == nil {
		fmt.Println("Spotify: not configured")
		return
	}

	resp, err := c.apiRequest("PUT", "/me/player/pause", nil)
	if err != nil {
		fmt.Printf("Spotify pause error: %s\n", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == 204 || resp.StatusCode == 200 {
		fmt.Println("  Spotify → paused")
	} else {
		body, _ := io.ReadAll(resp.Body)
		fmt.Printf("  Spotify pause HTTP %d: %s\n", resp.StatusCode, body)
	}
}
