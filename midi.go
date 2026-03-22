package main

import (
	"fmt"
	"os"
	"sync/atomic"
	"time"

	"gitlab.com/gomidi/midi/v2"
)

var PadLocked atomic.Bool
var RedMode atomic.Bool

const (
	PadChannel       = 9
	KnobChannel      = 0
	PitchBendChannel = 0
	KeyChannel       = 0 // ch 1: stationary LED for keys / non-drum controls (Novation)
	TransportChannel = 15

	// KeyChannelFlash / KeyChannelPulse — same note numbers as KeyChannel; only the channel selects LED mode.
	KeyChannelFlash = 1 // ch 2: flashing
	KeyChannelPulse = 2 // ch 3: pulsing (synced to MIDI clock / default tempo)

	DawModeChannel = 15
	DawModeNote    = 12

	PadChannelPulse = 11 // ch 12: pulsing pads only (drum mode)

	ColorOff   = 0
	ColorOn    = 21
	ColorNotOn = 53
	ColorError = 5

	ColorStatusGood          = 87
	ColorStatusBad           = 72
	ColorStatusIndeterminate = 13

	ColorPulseLoad = 45

	// Status keys: MIDI Port reports them as NoteOn ch 0 (Launchkey); DAW out uses the same note + velocity as palette index.
	StatusKeyBluetooth     = 119
	StatusKeyTV            = 103
	StatusKeyInternet      = 100
	StatusKeyLivingRoomCam = 97
	StatusKeyOfficeCam     = 96
	StatusKeyFlowerLamp    = 112
	StatusKeyMushroomLamp  = 113
)

var (
	stopMidiFn func()
	stopDawFn  func()
)

var midiControls = struct {
	PadSpeaker          uint8
	PadMushroomLamp     uint8
	PadFlowerLamp       uint8
	PadLivingRoomCamera uint8
	PadOfficeCamera     uint8
	PadTV               uint8
	PadRestartServer    uint8

	KeyNextTrack  uint8
	KeyPrevTrack  uint8
	KeyMuteTV     uint8
	KeyPingPhone  uint8
	KeyVolumeUp   uint8
	KeyVolumeDown uint8

	BrightnessMushroomLamp uint8
	MusicVolumeUp          uint8
	MusicVolumeDown        uint8
	Play                   uint8
	Stop                   uint8
}{
	PadSpeaker:          47,
	PadMushroomLamp:     37,
	PadFlowerLamp:       36,
	PadOfficeCamera:     40,
	PadLivingRoomCamera: 41,
	PadTV:               51,
	PadRestartServer:    48,

	KeyNextTrack:  66,
	KeyPrevTrack:  68,
	KeyMuteTV:     48,
	KeyPingPhone:  49,
	KeyVolumeUp:   71,
	KeyVolumeDown: 72,

	BrightnessMushroomLamp: 1,
	MusicVolumeUp:          105,
	MusicVolumeDown:        104,
	Play:                   115,
	Stop:                   117,
}

var send func(midi.Message) error

func midiInit(handleMIDI func(midi.Message, int32)) {
	fmt.Println("Initializing...")

	midiIn, err := midi.FindInPort("MIDI Port")
	if err != nil {
		fmt.Println("no Launchkey MIDI input port found")
		os.Exit(1)
	}

	dawIn, err := midi.FindInPort("DAW")
	if err != nil {
		fmt.Println("no Launchkey DAW input port found")
		os.Exit(1)
	}

	out, err := midi.FindOutPort("DAW")
	if err != nil {
		fmt.Println("no Launchkey DAW output port found")
		os.Exit(1)
	}

	sendTo, err := midi.SendTo(out)
	if err != nil {
		fmt.Printf("error opening DAW output: %s\n", err)
		os.Exit(1)
	}

	send = sendTo

	fmt.Printf("MIDI input:   %s\n", midiIn)
	fmt.Printf("DAW input:    %s\n", dawIn)
	fmt.Printf("DAW output:   %s\n\n", out)

	midiEnterDAWMode()

	stopMidi, err := midi.ListenTo(midiIn, handleMIDI, midi.UseSysEx())
	if err != nil {
		fmt.Printf("error listening on MIDI port: %s\n", err)
		midiExitDAWMode()
		os.Exit(1)
	}

	stopDaw, err := midi.ListenTo(dawIn, handleMIDI, midi.UseSysEx())
	if err != nil {
		fmt.Printf("error listening on DAW port: %s\n", err)
		stopMidi()
		midiExitDAWMode()
		os.Exit(1)
	}

	stopMidiFn = stopMidi
	stopDawFn = stopDaw
}

func midiPadColorForState(active bool) uint8 {
	if active {
		return ColorOn
	}
	return ColorNotOn
}

type DeviceStatus string

const (
	StatusGood          DeviceStatus = "good"
	StatusBad           DeviceStatus = "bad"
	StatusIndeterminate DeviceStatus = "indeterminate"
)

func midiSetPadColorDirect(pad, color uint8) {
	if send == nil {
		return
	}
	if err := send(midi.NoteOn(PadChannel, pad, color)); err != nil {
		fmt.Printf("error setting pad %d color: %s\n", pad, err)
	}
}

func midiStop() {
	if stopMidiFn != nil {
		stopMidiFn()
	}
	if stopDawFn != nil {
		stopDawFn()
	}
	blankAllPads()
	midiExitDAWMode()
}

func midiSetPadColor(pad, color uint8) {
	if PadLocked.Load() {
		return
	}
	midiSetPadColorDirect(pad, color)
}

// midiSetCCButtonColorDirect sets LED colour for a DAW-mode CC control (Launchkey: ch 16, value = palette / grayscale).
func midiSetCCButtonColorDirect(cc, color uint8) {
	if send == nil {
		return
	}
	if err := send(midi.ControlChange(DawModeChannel, cc, color)); err != nil {
		fmt.Printf("error setting CC %d colour: %s\n", cc, err)
	}
}

// midiSetStatusKeyColorDirect sets LED colour for keys that appear as NoteOn on MIDI Port ch 0 (not drum pads, not CC).
func midiSetStatusKeyColorDirect(note, color uint8) {
	if send == nil {
		return
	}
	if err := send(midi.NoteOn(KeyChannel, note, color)); err != nil {
		fmt.Printf("error setting status key %d colour: %s\n", note, err)
	}
}

func midiSetStatusKeyColor(note, color uint8) {
	if PadLocked.Load() {
		return
	}
	midiSetStatusKeyColorDirect(note, color)
}

func midiSetStatusKeyForDeviceStatus(note uint8, status DeviceStatus) {
	switch status {
	case StatusGood:
		midiSetStatusKeyPulse(note, ColorStatusGood)
	case StatusBad:
		midiSetStatusKeyPulse(note, ColorStatusBad)
	case StatusIndeterminate:
		midiSetStatusKeyPulse(note, ColorStatusIndeterminate)
	}
}

func midiSetStatusKeyPulse(note, color uint8) {
	if PadLocked.Load() {
		return
	}

	if send == nil {
		return
	}
	if err := send(midi.NoteOn(KeyChannelPulse, note, color)); err != nil {
		fmt.Printf("error setting status key %d pulse: %s\n", note, err)
	}
}

func midiSetPadPulse(pad, color uint8) {
	if PadLocked.Load() || send == nil {
		return
	}

	if err := send(midi.NoteOn(PadChannelPulse, pad, color)); err != nil {
		fmt.Printf("error setting pad %d pulse: %s\n", pad, err)

	}
}

func blankAllPads() {
	for pad := uint8(36); pad <= 51; pad++ {
		midiSetPadColorDirect(pad, ColorOff)
	}
}

var rainbowPalette = []uint8{5, 9, 13, 21, 33, 45, 53, 57}

func runPadAnimation() {
	const (
		duration  = 20 * time.Second
		frameRate = 50 * time.Millisecond
	)

	ticker := time.NewTicker(frameRate)
	defer ticker.Stop()

	deadline := time.After(duration)
	offset := 0

	for {
		for i := uint8(0); i < 16; i++ {
			c := rainbowPalette[(int(i)+offset)%len(rainbowPalette)]
			midiSetPadColorDirect(36+i, c)
		}
		offset++

		select {
		case <-deadline:
			return
		case <-ticker.C:
		}
	}
}

func midiEnterDAWMode() {
	if send == nil {
		return
	}
	if err := send(midi.NoteOn(DawModeChannel, DawModeNote, 127)); err != nil {
		fmt.Printf("error entering DAW mode: %s\n", err)
		return
	}
	if err := send(midi.ControlChange(DawModeChannel, 3, 1)); err != nil {
		fmt.Printf("error setting drum pad mode: %s\n", err)
	}
}

func midiExitDAWMode() {
	if send == nil {
		return
	}
	if err := send(midi.NoteOn(DawModeChannel, DawModeNote, 0)); err != nil {
		fmt.Printf("error exiting DAW mode: %s\n", err)
	}
}
