package main

import (
	"fmt"
	"sync/atomic"
	"time"

	"gitlab.com/gomidi/midi/v2"
)

var padLocked atomic.Bool

const (
	padChannel       = 9
	padFlowerLamp    = 36
	padMushroomLamp  = 37
	knobChannel      = 0
	knobMushroomCC   = 1
	pitchBendChannel = 0

	ccVolUp   = 105
	ccVolDown = 104

	keyChannel   = 0
	keyNextTrack = 66
	keyPrevTrack = 68
	keyMuteTV    = 48
	keyPingPhone = 49

	dawModeChannel = 15
	dawModeNote    = 12

	transportChannel = 15
	ccPlay           = 115
	ccStop           = 117

	padChannelPulse = 11

	colorOff   = 0
	colorOn    = 21
	colorNotOn = 13

	colorRed       = 5
	colorPulseLoad = 45
)

var send func(midi.Message) error

func setPadColorDirect(pad, color uint8) {
	if send == nil {
		return
	}
	if err := send(midi.NoteOn(padChannel, pad, color)); err != nil {
		fmt.Printf("error setting pad %d color: %s\n", pad, err)
	}
}

func setPadColor(pad, color uint8) {
	if padLocked.Load() {
		return
	}
	setPadColorDirect(pad, color)
}

func setPadPulse(pad, color uint8) {
	if padLocked.Load() || send == nil {
		return
	}
	if err := send(midi.NoteOn(padChannelPulse, pad, color)); err != nil {
		fmt.Printf("error setting pad %d pulse: %s\n", pad, err)
	}
}

func blankAllPads() {
	for pad := uint8(36); pad <= 51; pad++ {
		setPadColorDirect(pad, colorOff)
	}
}

var rainbowPalette = []uint8{5, 9, 13, 21, 33, 45, 53, 57}

func runPadAnimation() {
	const (
		duration  = 15 * time.Second
		frameRate = 50 * time.Millisecond
	)

	ticker := time.NewTicker(frameRate)
	defer ticker.Stop()

	deadline := time.After(duration)
	offset := 0

	for {
		for i := uint8(0); i < 16; i++ {
			c := rainbowPalette[(int(i)+offset)%len(rainbowPalette)]
			setPadColorDirect(36+i, c)
		}
		offset++

		select {
		case <-deadline:
			return
		case <-ticker.C:
		}
	}
}

func restoreAllPadColors() {
	mu.Lock()
	if flower, ok := bulbs[FLOWER_LAMP]; ok {
		setPadColorDirect(padFlowerLamp, lampPadColor(flower.on))
	} else {
		setPadColorDirect(padFlowerLamp, colorRed)
	}
	if mushroom, ok := bulbs[MUSHROOM_LAMP]; ok {
		setPadColorDirect(padMushroomLamp, lampPadColor(mushroom.on))
	} else {
		setPadColorDirect(padMushroomLamp, colorRed)
	}
	mu.Unlock()

	setPadColorDirect(padSpeaker, speakerPadColor(isSpeakerConnected()))
	setPadColorDirect(padTV, tvPadColor(isTVOn()))

	assigned := map[uint8]bool{
		padFlowerLamp: true, padMushroomLamp: true,
		padSpeaker: true, padTV: true,
	}
	for pad := uint8(36); pad <= 51; pad++ {
		if !assigned[pad] {
			setPadColorDirect(pad, colorOff)
		}
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
	if padLocked.Load() {
		return
	}

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
			case padTV:
				fmt.Printf("[%6dms] TOGGLE TV (vel=%d)\n", timestampms, vel)
				go toggleTV()
				return
			}
		}
		if ch == keyChannel {
			switch key {
			case keyNextTrack:
				fmt.Printf("[%6dms] NEXT TRACK\n", timestampms)
				go spotifyNext()
				return
			case keyPrevTrack:
				fmt.Printf("[%6dms] PREV TRACK\n", timestampms)
				go spotifyPrev()
				return
			case keyMuteTV:
				fmt.Printf("[%6dms] MUTE TV\n", timestampms)
				go toggleTVMute()
				return
			case keyPingPhone:
				fmt.Printf("[%6dms] PING IPHONE\n", timestampms)
				go pingIPhone()
				return
			}
		}
		fmt.Printf("[%6dms] NoteOn    ch=%d  key=%3d  vel=%3d  (%s)\n",
			timestampms, ch, key, vel, midi.Note(key))

	case msg.GetNoteEnd(&ch, &key):
		if ch == padChannel && (key == padFlowerLamp || key == padMushroomLamp || key == padSpeaker || key == padTV) {
			return
		}
		if ch == keyChannel && (key == keyNextTrack || key == keyPrevTrack || key == keyMuteTV || key == keyPingPhone) {
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
