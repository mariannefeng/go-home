package main

import (
	"fmt"
)

type noteBinding struct {
	ch    uint8
	key   uint8
	label string
	press func()
}

type ccBinding struct {
	ch    uint8
	cc    uint8
	label string
	onMax func()
	onAny func(val uint8)
}

type pitchBendBinding struct {
	ch       uint8
	label    string
	onChange func(rel int16)
}

var noteBindings = []noteBinding{
	// Pads
	{PadChannel, midiControls.PadFlowerLamp, "TOGGLE FLOWER LAMP", func() {
		on, err := kasaToggleLamp(FlowerLamp)
		if err == nil {
			midiSetPadColor(midiControls.PadFlowerLamp, midiPadColorForState(on))
		}
	}},
	{PadChannel, midiControls.PadMushroomLamp, "TOGGLE MUSHROOM LAMP", func() {
		on, err := kasaToggleLamp(MushroomLamp)
		if err == nil {
			midiSetPadColor(midiControls.PadMushroomLamp, midiPadColorForState(on))
		}
	}},
	{PadChannel, midiControls.PadSpeaker, "TOGGLE SPEAKER", func() {
		midiSetPadPulse(midiControls.PadSpeaker, ColorPulseLoad)
		connected, err := bluetoothToggle()
		if err == nil {
			midiSetPadColor(midiControls.PadSpeaker, midiPadColorForState(connected))
		} else {
			midiSetPadColor(midiControls.PadSpeaker, midiPadColorForState(bluetoothIsConnected()))
		}
	}},
	{PadChannel, midiControls.PadTV, "TOGGLE TV", func() {
		midiSetPadPulse(midiControls.PadTV, ColorPulseLoad)
		on, err := tvToggle()
		if err == nil {
			midiSetPadColor(midiControls.PadTV, midiPadColorForState(on))
		} else {
			midiSetPadColor(midiControls.PadTV, midiPadColorForState(tvIsOn()))
		}
	}},

	// Keys
	{KeyChannel, midiControls.KeyNextTrack, "NEXT TRACK", spotifyNext},
	{KeyChannel, midiControls.KeyPrevTrack, "PREV TRACK", spotifyPrev},
	{KeyChannel, midiControls.KeyMuteTV, "MUTE TV", func() { tvToggleMute() }},
	{KeyChannel, midiControls.KeyPingPhone, "PING IPHONE", func() {
		if !PadLocked.CompareAndSwap(false, true) {
			return
		}
		defer PadLocked.Store(false)

		if err := phonePing(); err != nil {
			fmt.Println("  iPhone ping:", err)
			return
		}
		runPadAnimation()
		resetPadColors()
	}},
}

var ccBindings = []ccBinding{
	{KnobChannel, midiControls.BrightnessMushroomLamp, "BRIGHTNESS MUSHROOM LAMP", nil, func(val uint8) {
		brightness := 100 - int(val)*100/127
		on, err := kasaSetBrightness(MushroomLamp, brightness)
		if err == nil {
			midiSetPadColor(midiControls.PadMushroomLamp, midiPadColorForState(on))
		}
	}},
	{KnobChannel, midiControls.VolumeUp, "VOL UP", func() { spotifyAdjustVolume(10) }, nil},
	{KnobChannel, midiControls.VolumeDown, "VOL DOWN", func() { spotifyAdjustVolume(-10) }, nil},
	{TransportChannel, midiControls.Play, "PLAY", spotifyPlay, nil},
	{TransportChannel, midiControls.Stop, "PAUSE", spotifyPause, nil},
}

var pitchBendBindings = []pitchBendBinding{
	{PitchBendChannel, "BRIGHTNESS FLOWER LAMP", func(rel int16) {
		brightness := 100 - int(rel+8192)*100/16383
		on, err := kasaSetBrightness(FlowerLamp, brightness)
		if err == nil {
			midiSetPadColor(midiControls.PadFlowerLamp, midiPadColorForState(on))
		}
	}},
}
