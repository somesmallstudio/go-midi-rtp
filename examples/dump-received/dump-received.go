package main

import (
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/grandcat/zeroconf"
	"github.com/laenzlinger/go-midi-rtp/rtp"
	"github.com/laenzlinger/go-midi-rtp/session"
)

func main() {
	port := 7005
	bonjourName := "rtpmidi-dumper"
	server, err := zeroconf.Register(bonjourName, "_apple-midi._udp", "local.", port, []string{"txtv=0", "lo=1", "la=2"}, nil)
	if err != nil {
		panic(err)
	}
	defer server.Shutdown()

	s := session.Start(bonjourName, uint16(port))
	s.Handle(func(msg rtp.MIDIMessage, s *session.MIDINetworkSession) {
		for _, cmd := range msg.Commands.Commands {
			fmt.Printf("Received MIDI command:\n%s", hex.Dump(cmd.Payload))
		}
	})

	sig := make(chan os.Signal, 1)

	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)

	run := true
	for run {
		select {
		case <-sig:
			run = false
		}
	}

	log.Println("Shutting down.")
	s.End()
}
