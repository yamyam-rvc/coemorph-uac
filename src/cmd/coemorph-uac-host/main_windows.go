//go:build windows

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"regexp"
	"syscall"

	"virtualcables/internal/hostsession"
	"virtualcables/internal/uac1"
	"virtualcables/internal/usbip"
	"virtualcables/internal/vhci"
)

func run() error {
	serial := flag.String("serial", "", "stable COEMORPH installation serial")
	flag.Parse()
	if flag.NArg() != 0 || !regexp.MustCompile(`^COEMORPH-[0-9A-F]{32}-001$`).MatchString(*serial) {
		return fmt.Errorf("expected -serial COEMORPH-<32 uppercase hexadecimal digits>-001")
	}
	// A product-owned redirected pipe is the lifetime lease. Refuse console,
	// files and NUL, which could keep a detached helper alive after product exit.
	typ, err := syscall.GetFileType(syscall.Handle(os.Stdin.Fd()))
	if err != nil || typ != syscall.FILE_TYPE_PIPE {
		return fmt.Errorf("stdin must be a product-owned pipe")
	}
	d, err := uac1.NewUAC2DeviceWithSerial(1, 250, *serial)
	if err != nil {
		return err
	}
	transport, err := vhci.Open()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _, _ = io.Copy(io.Discard, os.Stdin); cancel() }()
	logger := log.New(os.Stderr, "coemorph-uac ", log.LstdFlags|log.Lmicroseconds)
	server := usbip.NewServer("127.0.0.1:3240", []*uac1.Device{d}, logger)
	enc := json.NewEncoder(os.Stdout)
	err = hostsession.Run(ctx, transport, server, func(owned vhci.Device) error {
		return enc.Encode(struct {
			State  string      `json:"state"`
			Serial string      `json:"serial"`
			Device vhci.Device `json:"device"`
		}{"attached", *serial, owned})
	})
	if err != nil {
		return err
	}
	return enc.Encode(struct {
		State string `json:"state"`
	}{"stopped"})
}

func main() {
	if err := run(); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}
