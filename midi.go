package main

import (
	"fmt"

	"gitlab.com/gomidi/midi/v2"
)

const (
	padChannel       = 9
	padFlowerLamp    = 36
	padMushroomLamp  = 37
	knobChannel      = 0
	knobMushroomCC   = 1
	pitchBendChannel = 0

	ccVolUp   = 105
	ccVolDown = 104

	dawModeChannel = 15
	dawModeNote    = 12

	transportChannel = 15
	ccPlay           = 115
	ccStop           = 117

	padChannelPulse = 11

	colorOff    = 0
	colorRed    = 5
	colorGreen  = 21
	colorYellow = 13
)

var send func(midi.Message) error

func setPadColor(pad, color uint8) {
	if send == nil {
		return
	}
	if err := send(midi.NoteOn(padChannel, pad, color)); err != nil {
		fmt.Printf("error setting pad %d color: %s\n", pad, err)
	}
}

func setPadPulse(pad, color uint8) {
	if send == nil {
		return
	}
	if err := send(midi.NoteOn(padChannelPulse, pad, color)); err != nil {
		fmt.Printf("error setting pad %d pulse: %s\n", pad, err)
	}
}

func blankAllPads() {
	for pad := uint8(36); pad <= 51; pad++ {
		setPadColor(pad, colorOff)
	}
}

func enterDAWMode() {
	if send == nil {
		return
	}
	if err := send(midi.NoteOn(dawModeChannel, dawModeNote, 127)); err != nil {
		fmt.Printf("error entering DAW mode: %s\n", err)
		return
	}
	if err := send(midi.ControlChange(dawModeChannel, 3, 1)); err != nil {
		fmt.Printf("error setting drum pad mode: %s\n", err)
	}
}

func exitDAWMode() {
	if send == nil {
		return
	}
	if err := send(midi.NoteOn(dawModeChannel, dawModeNote, 0)); err != nil {
		fmt.Printf("error exiting DAW mode: %s\n", err)
	}
}

func handleMIDI(msg midi.Message, timestampms int32) {
	var ch, key, vel, cc, val uint8
	var pitchRel int16
	var bt []byte

	switch {
	case msg.GetNoteStart(&ch, &key, &vel):
		if ch == padChannel {
			switch key {
			case padFlowerLamp:
				fmt.Printf("[%6dms] TOGGLE flower lamp (vel=%d)\n", timestampms, vel)
				go toggleLamp(FLOWER_LAMP, padFlowerLamp)
				return
			case padMushroomLamp:
				fmt.Printf("[%6dms] TOGGLE mushroom lamp (vel=%d)\n", timestampms, vel)
				go toggleLamp(MUSHROOM_LAMP, padMushroomLamp)
				return
			case padSpeaker:
				fmt.Printf("[%6dms] TOGGLE speaker (vel=%d)\n", timestampms, vel)
				go toggleSpeaker()
				return
			}
		}
		fmt.Printf("[%6dms] NoteOn    ch=%d  key=%3d  vel=%3d  (%s)\n",
			timestampms, ch, key, vel, midi.Note(key))

	case msg.GetNoteEnd(&ch, &key):
		if ch == padChannel && (key == padFlowerLamp || key == padMushroomLamp || key == padSpeaker) {
			return
		}
		fmt.Printf("[%6dms] NoteOff   ch=%d  key=%3d           (%s)\n",
			timestampms, ch, key, midi.Note(key))

	case msg.GetPitchBend(&ch, &pitchRel, nil):
		if ch == pitchBendChannel {
			brightness := 100 - int(pitchRel+8192)*100/16383
			fmt.Printf("[%6dms] BRIGHTNESS flower lamp  %d%%\n", timestampms, brightness)
			go setLampBrightness(FLOWER_LAMP, brightness)
			return
		}
		fmt.Printf("[%6dms] PitchBend ch=%d  pitch=%d\n", timestampms, ch, pitchRel)

	case msg.GetControlChange(&ch, &cc, &val):
		if ch == knobChannel && cc == knobMushroomCC {
			brightness := 100 - int(val)*100/127
			fmt.Printf("[%6dms] BRIGHTNESS mushroom lamp  %d%%\n", timestampms, brightness)
			go setLampBrightness(MUSHROOM_LAMP, brightness)
			return
		}
		if ch == knobChannel && val == 127 {
			switch cc {
			case ccVolUp:
				fmt.Printf("[%6dms] VOL UP\n", timestampms)
				go spotifyAdjustVolume(10)
				return
			case ccVolDown:
				fmt.Printf("[%6dms] VOL DOWN\n", timestampms)
				go spotifyAdjustVolume(-10)
				return
			}
		}
		if ch == transportChannel && val == 127 {
			switch cc {
			case ccPlay:
				fmt.Printf("[%6dms] PLAY\n", timestampms)
				go spotifyPlay()
				return
			case ccStop:
				fmt.Printf("[%6dms] PAUSE\n", timestampms)
				go spotifyPause()
				return
			}
		}
		fmt.Printf("[%6dms] CC        ch=%d  cc=%3d   val=%3d\n", timestampms, ch, cc, val)

	case msg.GetAfterTouch(&ch, &val):
		fmt.Printf("[%6dms] AfterTch  ch=%d  val=%3d\n", timestampms, ch, val)

	case msg.GetSysEx(&bt):
		fmt.Printf("[%6dms] SysEx     % X\n", timestampms, bt)

	default:
		fmt.Printf("[%6dms] Other     %v\n", timestampms, msg)
	}
}
