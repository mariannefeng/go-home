package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"gitlab.com/gomidi/midi/v2"
	_ "gitlab.com/gomidi/midi/v2/drivers/rtmididrv"
)

func main() {
	defer midi.CloseDriver()

	kasaInit()
	spotifyInit()
	midiInit(handleMIDI)
	resetPadColors()

	stopPoll := make(chan struct{})
	startPollers(stopPoll)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig

	close(stopPoll)
	midiStop()
	fmt.Println("\nStopped.")
}

func handleMIDI(msg midi.Message, timestampms int32) {
	// ignore input while keys are locked out (e.g. when pinging phone)
	if PadLocked.Load() {
		return
	}

	var ch, key, vel, cc, val uint8
	var pitchRel int16
	var bt []byte

	switch {
	case msg.GetNoteStart(&ch, &key, &vel):
		for _, b := range noteBindings {
			if ch == b.ch && key == b.key {
				fmt.Printf("[%6dms] %s (vel=%d)\n", timestampms, b.label, vel)
				go b.press()
				return
			}
		}
		fmt.Printf("[%6dms] NoteOn    ch=%d  key=%3d  vel=%3d  (%s)\n",
			timestampms, ch, key, vel, midi.Note(key))

	case msg.GetNoteEnd(&ch, &key):
		fmt.Printf("[%6dms] (Ignoring) NoteOff   ch=%d  key=%3d  (%s)\n",
			timestampms, ch, key, midi.Note(key))
		return

	case msg.GetPitchBend(&ch, &pitchRel, nil):
		for _, b := range pitchBendBindings {
			if pitchRel == 0 {
				return // ignore exactly center since pitch springs back every time
			}

			if ch == b.ch {
				fmt.Printf("[%6dms] %s ch=%d  pitch=%d\n", timestampms, b.label, ch, pitchRel)
				go b.onChange(pitchRel)
				return
			}
		}
		fmt.Printf("[%6dms] ch=%d  pitch=%d\n", timestampms, ch, pitchRel)

	case msg.GetControlChange(&ch, &cc, &val):
		for _, b := range ccBindings {
			if ch == b.ch && cc == b.cc {
				if b.onAny != nil {
					fmt.Printf("[%6dms] %s  %d\n", timestampms, b.label, val)
					go b.onAny(val)
					return
				}
				if b.onMax != nil && val == 127 {
					fmt.Printf("[%6dms] %s\n", timestampms, b.label)
					go b.onMax()
					return
				}
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

func resetPadColors() {
	states := kasaGetBulbStates()
	midiSetPadColorDirect(midiControls.PadFlowerLamp, midiPadColorForState(states[FlowerLamp]))
	midiSetPadColorDirect(midiControls.PadMushroomLamp, midiPadColorForState(states[MushroomLamp]))
	midiSetPadColorDirect(midiControls.PadSpeaker, midiPadColorForState(bluetoothIsConnected()))
	midiSetPadColorDirect(midiControls.PadTV, midiPadColorForState(tvIsOn()))

	cameraStates := kasaGetCameraStates()
	if on, ok := cameraStates[LIVING_ROOM]; ok {
		midiSetPadColorDirect(midiControls.PadLivingRoomCamera, midiPadColorForState(on))
	}
	if on, ok := cameraStates[OFFICE]; ok {
		midiSetPadColorDirect(midiControls.PadOfficeCamera, midiPadColorForState(on))
	}

	// turn off unused pads
	assigned := map[uint8]bool{
		midiControls.PadFlowerLamp: true, midiControls.PadMushroomLamp: true,
		midiControls.PadSpeaker: true, midiControls.PadTV: true,
		midiControls.PadLivingRoomCamera: true, midiControls.PadOfficeCamera: true,
	}
	for pad := uint8(36); pad <= 51; pad++ {
		if !assigned[pad] {
			midiSetPadColorDirect(pad, ColorOff)
		}
	}
}

func pollEvery(interval time.Duration, stop <-chan struct{}, update func()) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			update()
		}
	}
}

func startPollers(stop <-chan struct{}) {
	go pollEvery(30*time.Second, stop, func() {
		midiSetPadColor(midiControls.PadSpeaker, midiPadColorForState(bluetoothIsConnected()))
	})
	go pollEvery(30*time.Second, stop, func() {
		states := kasaRefresh()
		midiSetPadColor(midiControls.PadFlowerLamp, midiPadColorForState(states[FlowerLamp]))
		midiSetPadColor(midiControls.PadMushroomLamp, midiPadColorForState(states[MushroomLamp]))
	})
	go pollEvery(30*time.Second, stop, func() {
		states := kasaRefreshCameras()
		if on, ok := states[LIVING_ROOM]; ok {
			midiSetPadColor(midiControls.PadLivingRoomCamera, midiPadColorForState(on))
		}
		if on, ok := states[OFFICE]; ok {
			midiSetPadColor(midiControls.PadOfficeCamera, midiPadColorForState(on))
		}
	})
	go pollEvery(30*time.Second, stop, func() {
		midiSetPadColor(midiControls.PadTV, midiPadColorForState(tvIsOn()))
	})
}
