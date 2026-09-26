package uac1

import (
	"bytes"
	"encoding/binary"
	"testing"
	"unicode/utf16"
)

func TestCoemorphProductStringControlResponse(t *testing.T) {
	for _, tc := range []struct {
		cable int
		want  string
	}{{1, "こえもーふ"}, {2, "こえもーふ 02"}} {
		d, err := NewDevice(tc.cable, 100)
		if err != nil {
			t.Fatal(err)
		}
		languages, status := d.HandleControl(SetupPacket{
			RequestType: 0x80,
			Request:     RequestGetDescriptor,
			Value:       uint16(DescriptorString) << 8,
			Length:      255,
		}, nil)
		if status != 0 || len(languages) != 6 || binary.LittleEndian.Uint16(languages[4:6]) != 0x0411 {
			t.Fatalf("cable %d: Japanese LANGID missing: status=%d data=%x", tc.cable, status, languages)
		}
		data, status := d.HandleControl(SetupPacket{
			RequestType: 0x80,
			Request:     RequestGetDescriptor,
			Value:       uint16(DescriptorString)<<8 | 2,
			Index:       0x0411,
			Length:      255,
		}, nil)
		if status != 0 || len(data) < 2 || int(data[0]) != len(data) || data[1] != DescriptorString || len(data)%2 != 0 {
			t.Fatalf("cable %d: malformed product descriptor: status=%d data=%x", tc.cable, status, data)
		}
		units := make([]uint16, (len(data)-2)/2)
		for i := range units {
			units[i] = binary.LittleEndian.Uint16(data[2+i*2:])
		}
		if got := string(utf16.Decode(units)); got != tc.want {
			t.Fatalf("cable %d: product=%q, want %q", tc.cable, got, tc.want)
		}
	}
}

func TestGetDeviceDescriptor(t *testing.T) {
	d, err := NewDevice(1, 100)
	if err != nil {
		t.Fatal(err)
	}
	data, status := d.HandleControl(SetupPacket{
		RequestType: 0x80,
		Request:     RequestGetDescriptor,
		Value:       uint16(DescriptorDevice) << 8,
		Length:      18,
	}, nil)
	if status != 0 || len(data) != 18 {
		t.Fatalf("status=%d length=%d", status, len(data))
	}
}

func TestSetAndGetInterface(t *testing.T) {
	d, _ := NewDevice(1, 100)
	d.HandleControl(SetupPacket{RequestType: 0x00, Request: RequestSetConfiguration, Value: 1}, nil)
	d.HandleControl(SetupPacket{RequestType: 0x01, Request: RequestSetInterface, Index: 1, Value: 1}, nil)
	data, status := d.HandleControl(SetupPacket{RequestType: 0x81, Request: RequestGetInterface, Index: 1, Length: 1}, nil)
	if status != 0 || len(data) != 1 || data[0] != 1 {
		t.Fatalf("status=%d data=%v", status, data)
	}
}

func TestSamplingRate(t *testing.T) {
	d, _ := NewDevice(1, 100)
	data, status := d.HandleControl(SetupPacket{RequestType: 0xA2, Request: AudioGetCur, Value: 0x0100, Index: 0x82, Length: 3}, nil)
	if status != 0 || len(data) != 3 {
		t.Fatalf("status=%d data=%v", status, data)
	}
	rate := uint32(data[0]) | uint32(data[1])<<8 | uint32(data[2])<<16
	if rate != 48000 {
		t.Fatalf("rate=%d", rate)
	}
	_ = binary.LittleEndian
}

func TestCaptureRestartDoesNotPlayStaleAudio(t *testing.T) {
	d, err := NewDevice(1, 100)
	if err != nil {
		t.Fatal(err)
	}
	set := func(iface, alt uint16) {
		t.Helper()
		if _, status := d.HandleControl(SetupPacket{RequestType: 1, Request: RequestSetInterface, Index: iface, Value: alt}, nil); status != 0 {
			t.Fatalf("set interface %d alt %d: status %d", iface, alt, status)
		}
	}
	if _, status := d.HandleControl(SetupPacket{Request: RequestSetConfiguration, Value: 1}, nil); status != 0 {
		t.Fatalf("set configuration: status %d", status)
	}
	set(1, 1) // playback
	set(2, 1) // capture
	if n := d.WritePlayback([]byte{1, 2, 3, 4}); n != 4 {
		t.Fatalf("initial playback wrote %d bytes", n)
	}
	initial := make([]byte, 4)
	if n := d.ReadCapture(initial); n != 4 || !bytes.Equal(initial, []byte{1, 2, 3, 4}) {
		t.Fatalf("initial capture: bytes=%d pcm=%v", n, initial)
	}

	set(2, 0) // stopping capture flushes the ring
	stale := []byte{5, 6, 7, 8}
	if n := d.WritePlayback(stale); n != len(stale) {
		t.Fatalf("playback while capture stopped completed %d bytes", n)
	}
	set(2, 1)
	fresh := []byte{9, 10, 11, 12}
	if n := d.WritePlayback(fresh); n != len(fresh) {
		t.Fatalf("playback after capture restart completed %d bytes", n)
	}
	captured := make([]byte, len(fresh))
	if n := d.ReadCapture(captured); n != len(fresh) || !bytes.Equal(captured, fresh) {
		t.Fatalf("capture after restart contains stale PCM: bytes=%d pcm=%v, want %v", n, captured, fresh)
	}
}

func TestWritePlaybackAcknowledgesAndDropsWhenPlaybackIsInactive(t *testing.T) {
	d, err := NewDevice(1, 100)
	if err != nil {
		t.Fatal(err)
	}
	pcm := []byte{1, 2, 3, 4}
	checkDiscard := func(stage string) {
		t.Helper()
		if got := d.WritePlayback(pcm); got != len(pcm) {
			t.Fatalf("%s: acknowledged %d bytes, want %d", stage, got, len(pcm))
		}
		if got := d.Buffer.Available(); got != 0 {
			t.Fatalf("%s: retained %d bytes while playback was inactive", stage, got)
		}
	}
	checkDiscard("unconfigured")
	if _, status := d.HandleControl(SetupPacket{Request: RequestSetConfiguration, Value: 1}, nil); status != 0 {
		t.Fatalf("set configuration: %d", status)
	}
	checkDiscard("alternate zero")
	if _, status := d.HandleControl(SetupPacket{RequestType: 1, Request: RequestSetInterface, Index: 1, Value: 1}, nil); status != 0 {
		t.Fatalf("start playback: %d", status)
	}
	checkDiscard("capture stopped")
}
