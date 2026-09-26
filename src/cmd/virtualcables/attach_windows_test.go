//go:build windows

package main

import (
	"reflect"
	"testing"
)

func TestUSBIPAttachArgumentsSelectReceiveMode(t *testing.T) {
	for _, tc := range []struct {
		mode string
		want []string
	}{
		{"zero-copy", []string{"attach", "-r", "127.0.0.1", "-b", "1-1"}},
		{"low-latency", []string{"attach", "-r", "127.0.0.1", "-b", "1-1", "--receive-mode=low-latency"}},
	} {
		got, err := usbipAttachArguments("1-1", tc.mode)
		if err != nil || !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("mode %q: args=%v, err=%v, want %v", tc.mode, got, err, tc.want)
		}
	}
	if _, err := usbipAttachArguments("1-1", "unknown"); err == nil {
		t.Fatal("invalid receive mode was accepted")
	}
}

func TestWarmRestartOnlySkipsBrokerForExactActiveCables(t *testing.T) {
	const one = `Imported USB devices
====================
Port 01: device in use at Full Speed(12Mbps)
         unknown vendor : unknown product (ffff:ca01)
           -> usbip://127.0.0.1:3240/1-1
           -> remote bus/dev: 001/001
           -> serial:
           -> mode: zero-copy
`
	if !ownCableAttachmentsMatch(one, 1, "zero-copy") {
		t.Fatal("exact existing cable was not recognized")
	}
	if ownCableAttachmentsMatch(one, 2, "zero-copy") || ownCableAttachmentsMatch(one, 1, "low-latency") {
		t.Fatal("missing cable or receive-mode change skipped administrator broker")
	}
	const extra = one + `Port 02: device in use at Full Speed(12Mbps)
           -> usbip://127.0.0.1:3240/1-2
           -> mode: zero-copy
`
	if !ownCableAttachmentsMatch(extra, 2, "zero-copy") || ownCableAttachmentsMatch(extra, 1, "zero-copy") {
		t.Fatal("cable count mismatch was not detected")
	}
	const stale = `Port 01: device disconnected
           -> usbip://127.0.0.1:3240/1-1
           -> mode: zero-copy
`
	if ownCableAttachmentsMatch(stale, 1, "zero-copy") {
		t.Fatal("disconnected port skipped administrator broker")
	}
}
