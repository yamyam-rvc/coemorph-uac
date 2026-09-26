package uac1

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestUAC2LabTopologyAndTransport(t *testing.T) {
	d, err := NewUAC2LabDevice("LAB-001")
	if err != nil {
		t.Fatal(err)
	}
	if got := d.Descriptors.InterfaceTuples(); len(got) != 3 || got[0] != [4]byte{1, 1, 0x20, 0} || got[1] != [4]byte{1, 2, 0x20, 0} || got[2] != got[1] {
		t.Fatalf("interfaces: %v", got)
	}
	old, _ := NewDevice(1, 250)
	if got := old.Descriptors.InterfaceTuples(); len(got) != 3 || got[0] != [4]byte{1, 1, 0, 0} || got[1] != [4]byte{1, 2, 0, 0} {
		t.Fatalf("legacy interfaces: %v", got)
	}
	var iface, alt byte
	acBytes, clockCount, endpoints := 0, 0, 0
	terminals := map[byte][]byte{}
	for p := 0; p < len(d.Descriptors.Config); {
		v := d.Descriptors.Config[p : p+int(d.Descriptors.Config[p])]
		p += len(v)
		if v[1] == DescriptorInterface {
			iface, alt = v[2], v[3]
			if (alt == 0 && v[4] != 0) || (alt == 1 && v[4] != 1) {
				t.Fatalf("bandwidth: %x", v)
			}
		}
		if v[1] == DescriptorCSInterface && iface == 0 {
			acBytes += len(v)
			switch v[2] {
			case 1:
				if len(v) != 9 || binary.LittleEndian.Uint16(v[6:]) != 75 {
					t.Fatalf("AC header: %x", v)
				}
			case 0x0a:
				clockCount++
				if v[3] != 10 || v[4] != 5 || v[5] != 5 {
					t.Fatalf("clock: %x", v)
				}
			case 2:
				if len(v) != 17 || v[7] != 10 || v[8] != 2 || binary.LittleEndian.Uint32(v[9:]) != 3 {
					t.Fatalf("input terminal: %x", v)
				}
				terminals[v[3]] = v
			case 3:
				if len(v) != 12 || v[8] != 10 {
					t.Fatalf("output terminal: %x", v)
				}
				terminals[v[3]] = v
			default:
				t.Fatalf("unexpected entity %x", v)
			}
		}
		if v[1] == DescriptorEndpoint {
			endpoints++
			if alt != 1 || len(v) != 7 || binary.LittleEndian.Uint16(v[4:]) != 192 || v[6] != 1 {
				t.Fatalf("endpoint: %x", v)
			}
			if (iface == 1 && (v[2] != 1 || v[3] != 9)) || (iface == 2 && (v[2] != 0x82 || v[3] != 0x0d)) {
				t.Fatalf("endpoint routing: %x", v)
			}
		}
	}
	if acBytes != 75 || clockCount != 1 || endpoints != 2 || len(terminals) != 4 {
		t.Fatalf("topology %d %d %d %v", acBytes, clockCount, endpoints, terminals)
	}
	if terminals[3][7] != 1 || terminals[6][7] != 4 {
		t.Fatal("terminal links")
	}
	// Exercise the shared PCM path with actual class-2 configuration/alternates.
	for _, s := range []SetupPacket{{Request: RequestSetConfiguration, Value: 1}, {Request: RequestSetInterface, Index: 1, Value: 1}, {Request: RequestSetInterface, Index: 2, Value: 1}} {
		if _, status := d.HandleControl(s, nil); status != 0 {
			t.Fatal(status)
		}
	}
	pcm := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	d.WritePlayback(pcm)
	got := make([]byte, len(pcm))
	d.ReadCapture(got)
	if !bytes.Equal(pcm, got) {
		t.Fatalf("PCM changed: %x", got)
	}
}

func TestUAC2LabClockControls(t *testing.T) {
	d, _ := NewUAC2LabDevice("LAB-001")
	for _, tc := range []struct {
		name   string
		setup  SetupPacket
		want   []byte
		status int32
	}{
		{"frequency", SetupPacket{0xa1, 1, 0x100, 0xa00, 4}, []byte{0x80, 0xbb, 0, 0}, 0},
		{"range", SetupPacket{0xa1, 2, 0x100, 0xa00, 14}, []byte{1, 0, 0x80, 0xbb, 0, 0, 0x80, 0xbb, 0, 0, 0, 0, 0, 0}, 0},
		{"range size", SetupPacket{0xa1, 2, 0x100, 0xa00, 2}, []byte{1, 0}, 0},
		{"valid", SetupPacket{0xa1, 1, 0x200, 0xa00, 1}, []byte{1}, 0},
		{"readonly", SetupPacket{0x21, 1, 0x100, 0xa00, 4}, nil, -32},
		{"legacy request", SetupPacket{0xa1, 0x81, 0x100, 0xa00, 4}, nil, -32},
		{"wrong entity", SetupPacket{0xa1, 1, 0x100, 0x200, 4}, nil, -32},
		{"wrong interface", SetupPacket{0xa1, 1, 0x100, 0xa01, 4}, nil, -32},
		{"wrong channel", SetupPacket{0xa1, 1, 0x101, 0xa00, 4}, nil, -32},
		{"endpoint rate", SetupPacket{0xa2, 1, 0x100, 0x82, 4}, nil, -32},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, status := d.HandleControl(tc.setup, nil)
			if status != tc.status || !bytes.Equal(got, tc.want) {
				t.Fatalf("got %x/%d want %x/%d", got, status, tc.want, tc.status)
			}
		})
	}
}
