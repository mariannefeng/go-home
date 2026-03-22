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
	{PadChannel, midiControls.PadRestartServer, "RESTART SERVER", func() {
		restartServer()
	}},
	{PadChannel, midiControls.PadLivingRoomCamera, "TOGGLE LIVING ROOM CAMERA", func() {
		midiSetPadPulse(midiControls.PadLivingRoomCamera, ColorPulseLoad)
		on, err := kasaToggleCamera(LIVING_ROOM)
		if err == nil {
			midiSetPadColor(midiControls.PadLivingRoomCamera, midiPadColorForState(on))
		}
	}},
	{PadChannel, midiControls.PadOfficeCamera, "TOGGLE OFFICE CAMERA", func() {
		midiSetPadPulse(midiControls.PadOfficeCamera, ColorPulseLoad)
		on, err := kasaToggleCamera(OFFICE)
		if err == nil {
			midiSetPadColor(midiControls.PadOfficeCamera, midiPadColorForState(on))
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
			fmt.Println("  TV toggle error:", err)
			midiSetPadColor(midiControls.PadTV, midiPadColorForState(tvIsOn()))
		}
	}},

	// Keys
	{KeyChannel, midiControls.KeyVolumeUp, "TV VOLUME UP", tvVolumeUp},
	{KeyChannel, midiControls.KeyVolumeDown, "TV VOLUME DOWN", tvVolumeDown},
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
		v := int(val)
		brightness := 100

		if v > MUSHROOM_THRESHOLD {
			brightness = 100 - (v-MUSHROOM_THRESHOLD)*100/(127-MUSHROOM_THRESHOLD)
		}

		on, err := kasaSetBrightness(MushroomLamp, brightness)
		if err == nil {
			midiSetPadColor(midiControls.PadMushroomLamp, midiPadColorForState(on))
		}
	}},
	{KnobChannel, midiControls.MusicVolumeUp, "MUSIC VOL UP", func() { spotifyAdjustVolume(5) }, nil},
	{KnobChannel, midiControls.MusicVolumeDown, "MUSIC VOL DOWN", func() { spotifyAdjustVolume(-5) }, nil},
	{TransportChannel, midiControls.Play, "PLAY", spotifyPlay, nil},
	{TransportChannel, midiControls.Stop, "PAUSE", spotifyPause, nil},
}

var pitchBendBindings = []pitchBendBinding{
	{PitchBendChannel, "BRIGHTNESS FLOWER LAMP", func(rel int16) {
		v := int(rel + 8192)
		brightness := 100

		// if we're above brightness threshold, decrease brightness relative to remaining pitch range
		if v > FLOWER_THRESHOLD {
			brightness = 100 - (v-FLOWER_THRESHOLD)*100/(16383-FLOWER_THRESHOLD)
		}

		on, err := kasaSetBrightness(FlowerLamp, brightness)
		if err == nil {
			midiSetPadColor(midiControls.PadFlowerLamp, midiPadColorForState(on))
		}
	}},
}

const FLOWER_THRESHOLD = 2457 // about ~15% of pitch range
const MUSHROOM_THRESHOLD = 20 // about ~15% of other slidey
