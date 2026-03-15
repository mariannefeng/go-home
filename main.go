package main

import (
	"fmt"
	"os"
	"os/signal"

	kasa "github.com/cloudkucooland/go-kasa"
	"gitlab.com/gomidi/midi/v2"
	_ "gitlab.com/gomidi/midi/v2/drivers/rtmididrv"
)

const (
	padChannel       = 9
	padFlowerLamp    = 36
	padMushroomLamp  = 37
	knobChannel      = 0
	knobMushroomCC   = 1
	pitchBendChannel = 0
)

func discoverKasa() {
	fmt.Println("Discovering Kasa devices...")
	devices, err := kasa.BroadcastDiscovery(3, 1)
	if err != nil {
		fmt.Printf("Kasa discovery error: %s\n", err)
		return
	}
	fmt.Printf("Found %d Kasa device(s):\n", len(devices))
	for ip, info := range devices {
		fmt.Printf("  %s: %s [%s] relay=%d brightness=%d%%\n",
			ip, info.Alias, info.Model, info.RelayState, info.Brightness)
	}
	fmt.Println()
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
				return
			case padMushroomLamp:
				fmt.Printf("[%6dms] TOGGLE mushroom lamp (vel=%d)\n", timestampms, vel)
				return
			}
		}
		fmt.Printf("[%6dms] NoteOn    ch=%d  key=%3d  vel=%3d  (%s)\n",
			timestampms, ch, key, vel, midi.Note(key))

	case msg.GetNoteEnd(&ch, &key):
		if ch == padChannel && (key == padFlowerLamp || key == padMushroomLamp) {
			return
		}
		fmt.Printf("[%6dms] NoteOff   ch=%d  key=%3d           (%s)\n",
			timestampms, ch, key, midi.Note(key))

	case msg.GetPitchBend(&ch, &pitchRel, nil):
		if ch == pitchBendChannel {
			fmt.Printf("[%6dms] BRIGHTNESS flower lamp  pitch=%d\n", timestampms, pitchRel)
			return
		}
		fmt.Printf("[%6dms] PitchBend ch=%d  pitch=%d\n", timestampms, ch, pitchRel)

	case msg.GetControlChange(&ch, &cc, &val):
		if ch == knobChannel && cc == knobMushroomCC {
			fmt.Printf("[%6dms] BRIGHTNESS mushroom lamp  val=%d\n", timestampms, val)
			return
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

func main() {
	defer midi.CloseDriver()

	discoverKasa()

	fmt.Println("Available MIDI input ports:")
	fmt.Println(midi.GetInPorts())
	fmt.Println()

	in, err := midi.FindInPort("Launchkey")
	if err != nil {
		fmt.Println("no Launchkey MIDI port found — available ports listed above")
		os.Exit(1)
	}

	fmt.Printf("Listening on: %s\n\n", in)

	stop, err := midi.ListenTo(in, handleMIDI, midi.UseSysEx())
	if err != nil {
		fmt.Printf("error listening: %s\n", err)
		os.Exit(1)
	}

	fmt.Println("Press Ctrl+C to quit.")
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	<-sig

	stop()
	fmt.Println("\nStopped.")
}
