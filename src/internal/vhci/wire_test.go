package vhci

import (
	"bytes"
	"encoding/binary"
	"os"
	"testing"
)

// These fixtures come from MSVC compiling the unmodified public C++ header,
// not from this Go encoder. Generator: PHASE4/tools/vhci_abi_098_20260924.cpp.
func TestPinnedCPPAttachABI(t *testing.T) {
	want, err := os.ReadFile("testdata/attach_098.bin")
	if err != nil {
		t.Fatal(err)
	}
	got, err := attachRequest("1-2", true)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("Go attach bytes differ from native C++ ABI fixture")
	}
	for _, bad := range []string{"", "1-0", "1-33", "2-1", "1-01", "1-1\x00other", "1-1/path"} {
		if _, err := attachRequest(bad, false); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestPinnedCPPDeviceABI(t *testing.T) {
	b, err := os.ReadFile("testdata/list_098.bin")
	if err != nil {
		t.Fatal(err)
	}
	devices, err := parseDevices(b)
	if err != nil {
		t.Fatal(err)
	}
	want := Device{Port: 7, BusID: "1-2", Host: "127.0.0.1", Service: "3240", Vendor: 0xffff, Product: 0xca01, SerialOverride: "OWNTEST", LowLatency: true}
	if len(devices) != 1 || devices[0] != want {
		t.Fatalf("decoded devices: %+v", devices)
	}
	if !devices[0].IsLocalLabCable() {
		t.Fatal("local lab cable not recognized")
	}
	for name, edit := range map[string]func([]byte){
		"ABI":                func(v []byte) { v[0] = 0 },
		"zero port":          func(v []byte) { binary.LittleEndian.PutUint32(v[4:], 0) },
		"all ports sentinel": func(v []byte) { binary.LittleEndian.PutUint32(v[4:], 0xffffffff) },
		"unterminated host": func(v []byte) {
			for i := 4 + 68; i < 4+1093; i++ {
				v[i] = 'x'
			}
		},
		"invalid flag": func(v []byte) { v[4+1125] = 2 },
	} {
		t.Run(name, func(t *testing.T) {
			bad := append([]byte(nil), b...)
			edit(bad)
			if _, err := parseDevices(bad); err == nil {
				t.Fatal("malformed response accepted")
			}
		})
	}
	for _, n := range []int{0, 1, 3, 5, len(b) - 1} {
		if _, err := parseDevices(b[:n]); err == nil {
			t.Errorf("accepted %d bytes", n)
		}
	}
	duplicate := append(append([]byte(nil), b...), b[4:]...)
	if _, err := parseDevices(duplicate); err == nil {
		t.Fatal("duplicate port accepted")
	}
}

func TestDetachOwnershipScope(t *testing.T) {
	d := Device{Port: 7, BusID: "1-2", Host: "127.0.0.1", Service: "3240", Vendor: 0xffff, Product: 0xca01}
	for name, edit := range map[string]func(*Device){
		"remote":        func(v *Device) { v.Host = "192.0.2.1" },
		"other service": func(v *Device) { v.Service = "3241" },
		"other device":  func(v *Device) { v.Vendor = 0x1234 },
		"other bus":     func(v *Device) { v.BusID = "2-1" },
		"all ports":     func(v *Device) { v.Port = 0xffffffff },
	} {
		t.Run(name, func(t *testing.T) {
			changed := d
			edit(&changed)
			if changed.IsLocalLabCable() {
				t.Fatal("unrelated device accepted")
			}
		})
	}
}
