package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

type spotifyClient struct {
	clientID     string
	clientSecret string
	refreshToken string
	deviceName   string

	mu          sync.Mutex
	accessToken string
	tokenExpiry time.Time
}

var spotify *spotifyClient

func initSpotify() {
	id := os.Getenv("SPOTIFY_CLIENT_ID")
	secret := os.Getenv("SPOTIFY_CLIENT_SECRET")
	refresh := os.Getenv("SPOTIFY_REFRESH_TOKEN")
	device := os.Getenv("SPOTIFY_DEVICE_NAME")

	if id == "" || secret == "" || refresh == "" {
		fmt.Println("Spotify: missing SPOTIFY_CLIENT_ID, SPOTIFY_CLIENT_SECRET, or SPOTIFY_REFRESH_TOKEN — disabled")
		return
	}
	if device == "" {
		device = "sauron"
	}

	spotify = &spotifyClient{
		clientID:     id,
		clientSecret: secret,
		refreshToken: refresh,
		deviceName:   device,
	}

	if err := spotify.refresh(); err != nil {
		fmt.Printf("Spotify: initial token refresh failed: %s — disabled\n", err)
		spotify = nil
		return
	}
	fmt.Printf("Spotify: authenticated (device=%q)\n\n", device)
}

func (s *spotifyClient) refresh() error {
	data := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {s.refreshToken},
	}

	req, err := http.NewRequest("POST", "https://accounts.spotify.com/api/token", strings.NewReader(data.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString(
		[]byte(s.clientID+":"+s.clientSecret),
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

	s.mu.Lock()
	s.accessToken = result.AccessToken
	s.tokenExpiry = time.Now().Add(time.Duration(result.ExpiresIn-60) * time.Second)
	s.mu.Unlock()

	return nil
}

func (s *spotifyClient) token() (string, error) {
	s.mu.Lock()
	if time.Now().Before(s.tokenExpiry) {
		t := s.accessToken
		s.mu.Unlock()
		return t, nil
	}
	s.mu.Unlock()

	if err := s.refresh(); err != nil {
		return "", err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	return s.accessToken, nil
}

func (s *spotifyClient) apiRequest(method, path string, body io.Reader) (*http.Response, error) {
	tok, err := s.token()
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

func (s *spotifyClient) findDeviceID() (string, error) {
	resp, err := s.apiRequest("GET", "/me/player/devices", nil)
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
		if strings.EqualFold(d.Name, s.deviceName) {
			return d.ID, nil
		}
	}
	names := make([]string, len(result.Devices))
	for i, d := range result.Devices {
		names[i] = d.Name
	}
	return "", fmt.Errorf("device %q not found (available: %v)", s.deviceName, names)
}

func spotifyPlay() {
	if spotify == nil {
		fmt.Println("Spotify: not configured")
		return
	}

	deviceID, err := spotify.findDeviceID()
	if err != nil {
		fmt.Printf("Spotify play error: %s\n", err)
		return
	}

	resp, err := spotify.apiRequest("PUT", "/me/player/play?device_id="+deviceID, nil)
	if err != nil {
		fmt.Printf("Spotify play error: %s\n", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == 204 || resp.StatusCode == 200 {
		fmt.Println("  Spotify → playing")
	} else {
		body, _ := io.ReadAll(resp.Body)
		fmt.Printf("  Spotify play HTTP %d: %s\n", resp.StatusCode, body)
	}
}

func spotifyPause() {
	if spotify == nil {
		fmt.Println("Spotify: not configured")
		return
	}

	resp, err := spotify.apiRequest("PUT", "/me/player/pause", nil)
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
