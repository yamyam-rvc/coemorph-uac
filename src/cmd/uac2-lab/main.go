package main

import (
	"context"
	"flag"
	"log"
	"os"
	"time"

	"virtualcables/internal/uac1"
	"virtualcables/internal/usbip"
)

func main() {
	lifetime := flag.Duration("serve-for", 30*time.Second, "bounded laboratory server lifetime (1..60 seconds)")
	flag.Parse()
	if *lifetime < time.Second || *lifetime > time.Minute || flag.NArg() != 0 {
		log.Fatal("expected -serve-for between 1s and 60s, no positional arguments")
	}
	d, err := uac1.NewUAC2LabDevice("COEMORPH-UAC2-LAB-20260924-001")
	if err != nil {
		log.Fatal(err)
	}
	s := usbip.NewServer("127.0.0.1:3240", []*uac1.Device{d}, log.New(os.Stderr, "uac2-lab ", log.LstdFlags|log.Lmicroseconds))
	ctx, cancel := context.WithTimeout(context.Background(), *lifetime)
	defer cancel()
	if err := s.Serve(ctx); err != nil {
		log.Fatal(err)
	}
}
