package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

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

	dawModeChannel = 15
	dawModeNote    = 12

	colorOff   = 0
	colorRed   = 5
	colorGreen = 21
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

func discoverKasa() map[string]*kasa.Sysinfo {
	fmt.Println("Discovering Kasa devices...")
	devices, err := kasa.BroadcastDiscovery(3, 1)
	if err != nil {
		fmt.Printf("Kasa discovery error: %s\n", err)
		return nil
	}
	fmt.Printf("Found %d Kasa device(s):\n", len(devices))
	for ip, info := range devices {
		fmt.Printf("  %s: %+v\n", ip, *info)
	}

	fmt.Println("\nQuerying each device individually...")
	for ip := range devices {
		dev, err := kasa.NewDevice(ip)
		if err != nil {
			fmt.Printf("  %s: error creating device: %s\n", ip, err)
			continue
		}
		settings, err := dev.GetSettings()
		if err != nil {
			fmt.Printf("  %s: error getting settings: %s\n", ip, err)
			continue
		}
		fmt.Printf("  %s: %+v\n", ip, *settings)
	}
	fmt.Println()
	return devices
}

func updateLampPads(devices map[string]*kasa.Sysinfo) {
	if devices == nil {
		setPadColor(padFlowerLamp, colorRed)
		setPadColor(padMushroomLamp, colorRed)
		return
	}

	flowerFound, mushroomFound := false, false
	for _, info := range devices {
		switch info.Alias {
		case FLOWER_LAMP:
			flowerFound = true
			if info.RelayState == 1 {
				setPadColor(padFlowerLamp, colorGreen)
			} else {
				setPadColor(padFlowerLamp, colorRed)
			}
		case MUSHROOM_LAMP:
			mushroomFound = true
			if info.RelayState == 1 {
				setPadColor(padMushroomLamp, colorGreen)
			} else {
				setPadColor(padMushroomLamp, colorRed)
			}
		}
	}

	if !flowerFound {
		setPadColor(padFlowerLamp, colorRed)
	}
	if !mushroomFound {
		setPadColor(padMushroomLamp, colorRed)
	}
}

const (
	MUSHROOM_LAMP = "Mushroom bulb"
	FLOWER_LAMP   = "Flower lamp"
)

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

	devices := discoverKasa()

	fmt.Println("Available MIDI input ports:")
	fmt.Println(midi.GetInPorts())
	fmt.Println("Available MIDI output ports:")
	fmt.Println(midi.GetOutPorts())
	fmt.Println()

	in, err := midi.FindInPort("Launchkey")
	if err != nil {
		fmt.Println("no Launchkey MIDI input port found")
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

	fmt.Printf("Listening on: %s\n", in)
	fmt.Printf("LED output:   %s\n\n", out)

	enterDAWMode()
	blankAllPads()
	updateLampPads(devices)

	stop, err := midi.ListenTo(in, handleMIDI, midi.UseSysEx())
	if err != nil {
		fmt.Printf("error listening: %s\n", err)
		exitDAWMode()
		os.Exit(1)
	}

	fmt.Println("Ready.")
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig

	stop()
	blankAllPads()
	exitDAWMode()
	fmt.Println("\nStopped.")
}
