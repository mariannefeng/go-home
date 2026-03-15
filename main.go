package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"gitlab.com/gomidi/midi/v2"
	_ "gitlab.com/gomidi/midi/v2/drivers/rtmididrv"
)

func main() {
	defer midi.CloseDriver()

	bulbs = initKasa()
	initSpotify()

	fmt.Println("Available MIDI input ports:")
	fmt.Println(midi.GetInPorts())
	fmt.Println("Available MIDI output ports:")
	fmt.Println(midi.GetOutPorts())
	fmt.Println()

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

	send, err = midi.SendTo(out)
	if err != nil {
		fmt.Printf("error opening DAW output: %s\n", err)
		os.Exit(1)
	}

	fmt.Printf("MIDI input:   %s\n", midiIn)
	fmt.Printf("DAW input:    %s\n", dawIn)
	fmt.Printf("DAW output:   %s\n\n", out)

	speakerStatus := checkSpeakerStatus()
	fmt.Printf("Speaker (%s): %s\n\n", speakerName, []string{"off", "on (not connected)", "connected"}[speakerStatus])

	enterDAWMode()
	blankAllPads()
	updateLampPads(bulbs)
	setPadColor(padSpeaker, speakerPadColor(speakerStatus))

	stopMidi, err := midi.ListenTo(midiIn, handleMIDI, midi.UseSysEx())
	if err != nil {
		fmt.Printf("error listening on MIDI port: %s\n", err)
		exitDAWMode()
		os.Exit(1)
	}

	stopDaw, err := midi.ListenTo(dawIn, handleMIDI, midi.UseSysEx())
	if err != nil {
		fmt.Printf("error listening on DAW port: %s\n", err)
		stopMidi()
		exitDAWMode()
		os.Exit(1)
	}

	stopBtPoll := make(chan struct{})
	go pollSpeakerStatus(stopBtPoll)

	fmt.Println("Ready.")
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig

	close(stopBtPoll)
	stopMidi()
	stopDaw()
	blankAllPads()
	exitDAWMode()
	fmt.Println("\nStopped.")
}
