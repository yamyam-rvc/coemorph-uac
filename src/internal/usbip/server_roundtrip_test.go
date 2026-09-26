package usbip

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"log"
	"net"
	"testing"
	"time"

	"virtualcables/internal/uac1"
)

// This exercises both audio directions over the USB/IP wire without attaching
// a Windows driver. Ten 192-byte packets represent 10 ms of 48 kHz stereo PCM16.
func TestPlaybackToCapturePCMOverUSBIPStream(t *testing.T) {
	device, err := uac1.NewDeviceWithSerial(1, 100, "COEMORPH-ROUNDTRIP-001")
	if err != nil {
		t.Fatal(err)
	}
	for _, setup := range []uac1.SetupPacket{
		{Request: uac1.RequestSetConfiguration, Value: 1},
		{RequestType: 1, Request: uac1.RequestSetInterface, Value: 1, Index: 1},
		{RequestType: 1, Request: uac1.RequestSetInterface, Value: 1, Index: 2},
	} {
		if _, status := device.HandleControl(setup, nil); status != StatusOK {
			t.Fatalf("activate audio interface: %d", status)
		}
	}

	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()
	if err := clientConn.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	server := NewServer("127.0.0.1:0", []*uac1.Device{device}, log.New(io.Discard, "", 0))
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		server.handleURBs(context.Background(), bufio.NewReader(serverConn), serverConn, device)
	}()

	const packetCount = 10
	const packetBytes = 192
	const transferBytes = packetCount * packetBytes
	pcm := make([]byte, transferBytes)
	for i := range pcm {
		pcm[i] = byte(i*37 + 11)
	}
	submit := func(sequence, direction, endpoint uint32, payload []byte) {
		t.Helper()
		req := SubmitRequest{
			Basic:                BasicHeader{Command: CmdSubmit, Sequence: sequence, Direction: direction, Endpoint: endpoint},
			TransferBufferLength: transferBytes,
			NumberOfPackets:      packetCount,
			Interval:             1,
		}
		if err := binary.Write(clientConn, binary.BigEndian, req); err != nil {
			t.Fatal(err)
		}
		if len(payload) > 0 {
			if _, err := clientConn.Write(payload); err != nil {
				t.Fatal(err)
			}
		}
		for i := uint32(0); i < packetCount; i++ {
			if err := binary.Write(clientConn, binary.BigEndian, IsoPacket{Offset: i * packetBytes, Length: packetBytes}); err != nil {
				t.Fatal(err)
			}
		}
	}
	readReply := func(sequence uint32, wantPayload []byte) {
		t.Helper()
		frame := make([]byte, 48+len(wantPayload)+packetCount*16)
		if _, err := io.ReadFull(clientConn, frame); err != nil {
			t.Fatal(err)
		}
		if command, got := binary.BigEndian.Uint32(frame[0:4]), binary.BigEndian.Uint32(frame[4:8]); command != RetSubmit || got != sequence {
			t.Fatalf("reply command=%d sequence=%d, want RET_SUBMIT %d", command, got, sequence)
		}
		if status, actual := binary.BigEndian.Uint32(frame[20:24]), binary.BigEndian.Uint32(frame[24:28]); status != 0 || actual != transferBytes {
			t.Fatalf("reply status=%d actual=%d, want 0 and %d", status, actual, transferBytes)
		}
		if got := frame[48 : 48+len(wantPayload)]; !bytes.Equal(got, wantPayload) {
			t.Fatalf("capture PCM differed from playback: got first 16 %x, want %x", got[:16], wantPayload[:16])
		}
		for i := 0; i < packetCount; i++ {
			packet := frame[48+len(wantPayload)+i*16:]
			if offset, length, actual, status := binary.BigEndian.Uint32(packet[0:4]), binary.BigEndian.Uint32(packet[4:8]), binary.BigEndian.Uint32(packet[8:12]), binary.BigEndian.Uint32(packet[12:16]); offset != uint32(i*packetBytes) || length != packetBytes || actual != packetBytes || status != 0 {
				t.Fatalf("packet %d: offset=%d length=%d actual=%d status=%d", i, offset, length, actual, status)
			}
		}
	}

	submit(81, DirectionOut, 1, pcm)
	readReply(81, nil)
	submit(82, DirectionIn, 2, nil)
	readReply(82, pcm)
	if available, dropped, underruns := device.Buffer.Stats(); available != 0 || dropped != 0 || underruns != 0 {
		t.Fatalf("ring after roundtrip: available=%d dropped=%d underruns=%d", available, dropped, underruns)
	}
	clientConn.Close()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("USB/IP session did not close")
	}
}
