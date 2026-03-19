package main

import (
	"fmt"
	"os"
	"sync/atomic"
	"time"

	"gitlab.com/gomidi/midi/v2"
)

var PadLocked atomic.Bool

const (
	PadChannel       = 9
	KnobChannel      = 0
	PitchBendChannel = 0
	KeyChannel       = 0
	TransportChannel = 15

	DawModeChannel = 15
	DawModeNote    = 12

	PadChannelPulse = 11

	ColorOff   = 0
	ColorOn    = 21
	ColorNotOn = 53

	ColorPulseLoad = 45
)

var (
	stopMidiFn func()
	stopDawFn  func()
)

var midiControls = struct {
	PadSpeaker      uint8
	PadMushroomLamp uint8
	PadFlowerLamp   uint8
	PadTV           uint8

	KeyNextTrack uint8
	KeyPrevTrack uint8
	KeyMuteTV    uint8
	KeyPingPhone uint8

	BrightnessMushroomLamp uint8
	VolumeUp               uint8
	VolumeDown             uint8
	Play                   uint8
	Stop                   uint8
}{
	PadSpeaker:      47,
	PadMushroomLamp: 37,
	PadFlowerLamp:   36,
	PadTV:           51,

	KeyNextTrack: 66,
	KeyPrevTrack: 68,
	KeyMuteTV:    48,
	KeyPingPhone: 49,

	BrightnessMushroomLamp: 1,
	VolumeUp:               105,
	VolumeDown:             104,
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
		duration  = 20 * time.Minute
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
