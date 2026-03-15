package main

import (
	"fmt"
	"os"
	"os/signal"

	"gitlab.com/gomidi/midi/v2"
	_ "gitlab.com/gomidi/midi/v2/drivers/rtmididrv"
)

func main() {
	defer midi.CloseDriver()

	fmt.Println("Available MIDI input ports:")
	fmt.Println(midi.GetInPorts())
	fmt.Println()

	in, err := midi.FindInPort("Launchkey")
	if err != nil {
		fmt.Println("no Launchkey MIDI port found — available ports listed above")
		os.Exit(1)
	}

	fmt.Printf("Listening on: %s\n\n", in)

	stop, err := midi.ListenTo(in, func(msg midi.Message, timestampms int32) {
		var ch, key, vel, cc, val uint8
		var bt []byte

		switch {
		case msg.GetNoteStart(&ch, &key, &vel):
			fmt.Printf("[%6dms] NoteOn    ch=%d  key=%3d  vel=%3d  (note %s)\n",
				timestampms, ch, key, vel, midi.Note(key))
		case msg.GetNoteEnd(&ch, &key):
			fmt.Printf("[%6dms] NoteOff   ch=%d  key=%3d           (note %s)\n",
				timestampms, ch, key, midi.Note(key))
		case msg.GetControlChange(&ch, &cc, &val):
			fmt.Printf("[%6dms] CC        ch=%d  cc=%3d   val=%3d\n",
				timestampms, ch, cc, val)
		case msg.GetAfterTouch(&ch, &val):
			fmt.Printf("[%6dms] AfterTch  ch=%d  val=%3d\n",
				timestampms, ch, val)
		case msg.GetSysEx(&bt):
			fmt.Printf("[%6dms] SysEx     % X\n", timestampms, bt)
		default:
			fmt.Printf("[%6dms] Other     %v\n", timestampms, msg)
		}
	}, midi.UseSysEx())

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
