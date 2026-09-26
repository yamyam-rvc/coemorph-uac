package usbip

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"log"
	"net"
	"sync"
	"testing"
	"time"

	"virtualcables/internal/uac1"
)

func TestIsoCompletionsAreSerializedPerEndpoint(t *testing.T) {
	state := newConnectionState(io.Discard)
	now := time.Unix(100, 0)
	first := state.reserveIsoCompletion(2, 10, now)
	second := state.reserveIsoCompletion(2, 10, now)
	otherEndpoint := state.reserveIsoCompletion(1, 10, now)

	if got, want := first.Sub(now), 10*time.Millisecond; got != want {
		t.Fatalf("first deadline=%s, want %s", got, want)
	}
	if got, want := second.Sub(now), 20*time.Millisecond; got != want {
		t.Fatalf("second deadline=%s, want %s", got, want)
	}
	if got, want := otherEndpoint.Sub(now), 10*time.Millisecond; got != want {
		t.Fatalf("independent endpoint deadline=%s, want %s", got, want)
	}
}

func TestPlaybackCatchesUpOnlyAcrossShortDelay(t *testing.T) {
	state := newConnectionState(io.Discard)
	now := time.Unix(100, 0)
	if got := state.reserveIsoCompletion(1, 10, now); !got.Equal(now.Add(10 * time.Millisecond)) {
		t.Fatalf("first OUT deadline=%s", got)
	}
	if got := state.reserveIsoCompletion(1, 10, now.Add(15*time.Millisecond)); !got.Equal(now.Add(20 * time.Millisecond)) {
		t.Fatalf("short OUT catch-up deadline=%s", got)
	}
	idle := now.Add(50 * time.Millisecond)
	if got := state.reserveIsoCompletion(1, 10, idle); !got.Equal(idle.Add(10 * time.Millisecond)) {
		t.Fatalf("OUT idle reset deadline=%s", got)
	}
	if got := state.reserveIsoCompletion(2, 10, now); !got.Equal(now.Add(10 * time.Millisecond)) {
		t.Fatalf("first IN deadline=%s", got)
	}
	if got := state.reserveIsoCompletion(2, 10, now.Add(15*time.Millisecond)); !got.Equal(now.Add(25 * time.Millisecond)) {
		t.Fatalf("IN must not catch up, deadline=%s", got)
	}
}

func TestIsoResponsePreservesRequestStartFrame(t *testing.T) {
	device, err := uac1.NewDevice(1, 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, frame := range []uint32{0, 12345} {
		job := parsedSubmit{
			req: SubmitRequest{
				Basic:                BasicHeader{Command: CmdSubmit, Sequence: 71, Endpoint: 2, Direction: DirectionIn},
				TransferBufferLength: 4,
				NumberOfPackets:      1,
				StartFrame:           frame,
			},
			packets: []IsoPacket{{Length: 4}},
		}
		var wire bytes.Buffer
		if err := (&Server{}).processSubmit(context.Background(), newConnectionState(&wire), device, job); err != nil {
			t.Fatal(err)
		}
		if got := binary.BigEndian.Uint32(wire.Bytes()[28:32]); got != frame {
			t.Fatalf("wire response frame=%d, want request frame %d", got, frame)
		}
	}
}

func TestCapturePCMAndRepliesKeepArrivalOrderWhenSecondWorkerRunsFirst(t *testing.T) {
	device, err := uac1.NewDevice(1, 100)
	if err != nil {
		t.Fatal(err)
	}
	if _, status := device.HandleControl(uac1.SetupPacket{Request: uac1.RequestSetConfiguration, Value: 1}, nil); status != StatusOK {
		t.Fatalf("set configuration: %d", status)
	}
	if _, status := device.HandleControl(uac1.SetupPacket{RequestType: 1, Request: uac1.RequestSetInterface, Value: 1, Index: 2}, nil); status != StatusOK {
		t.Fatalf("set capture interface: %d", status)
	}
	if !device.CaptureActive() {
		t.Fatal("capture interface inactive")
	}
	pcm := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	device.Buffer.Write(pcm)

	var wire bytes.Buffer
	state := newConnectionState(&wire)
	server := &Server{}
	job := func(sequence uint32) parsedSubmit {
		previous, done := state.reserveIsoTurn(2)
		return parsedSubmit{
			req: SubmitRequest{
				Basic:                BasicHeader{Command: CmdSubmit, Sequence: sequence, Endpoint: 2, Direction: DirectionIn},
				TransferBufferLength: 4,
				NumberOfPackets:      1,
			},
			packets:      []IsoPacket{{Length: 4}},
			turnPrevious: previous,
			turnDone:     done,
		}
	}
	first, second := job(11), job(12)
	secondFinished := make(chan error, 1)
	go func() { secondFinished <- server.processOrderedIso(context.Background(), state, device, second) }()
	select {
	case err := <-secondFinished:
		t.Fatalf("second request completed before the first began: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	if err := server.processOrderedIso(context.Background(), state, device, first); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-secondFinished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("second request did not complete after the first")
	}

	// Each RET_SUBMIT has a 48-byte header, four PCM bytes and one 16-byte
	// isochronous packet descriptor. Verify both sequence IDs and PCM blocks.
	const frameLength = 48 + 4 + 16
	if wire.Len() != 2*frameLength {
		t.Fatalf("wire length=%d, want %d", wire.Len(), 2*frameLength)
	}
	for i, sequence := range []uint32{11, 12} {
		frame := wire.Bytes()[i*frameLength : (i+1)*frameLength]
		if got := binary.BigEndian.Uint32(frame[4:8]); got != sequence {
			t.Fatalf("reply %d sequence=%d, want %d", i, got, sequence)
		}
		if got := frame[48:52]; !bytes.Equal(got, pcm[i*4:(i+1)*4]) {
			t.Fatalf("reply %d PCM=%v, want %v", i, got, pcm[i*4:(i+1)*4])
		}
	}
}

func TestTwoCaptureSUBMITsOverUSBIPStream(t *testing.T) {
	device, err := uac1.NewDevice(1, 100)
	if err != nil {
		t.Fatal(err)
	}
	device.HandleControl(uac1.SetupPacket{Request: uac1.RequestSetConfiguration, Value: 1}, nil)
	device.HandleControl(uac1.SetupPacket{RequestType: 1, Request: uac1.RequestSetInterface, Value: 1, Index: 2}, nil)
	pcm := []byte{10, 11, 12, 13, 20, 21, 22, 23}
	device.Buffer.Write(pcm)
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()
	if err := clientConn.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	server := NewServer("127.0.0.1:0", []*uac1.Device{device}, log.New(io.Discard, "", 0))
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		server.handleURBs(context.Background(), bufio.NewReader(serverConn), serverConn, device)
	}()

	for _, sequence := range []uint32{41, 42} {
		request := SubmitRequest{
			Basic:                BasicHeader{Command: CmdSubmit, Sequence: sequence, Direction: DirectionIn, Endpoint: 2},
			TransferBufferLength: 4,
			NumberOfPackets:      1,
			Interval:             1,
		}
		if err := binary.Write(clientConn, binary.BigEndian, request); err != nil {
			t.Fatal(err)
		}
		if err := binary.Write(clientConn, binary.BigEndian, IsoPacket{Length: 4}); err != nil {
			t.Fatal(err)
		}
	}
	for i, sequence := range []uint32{41, 42} {
		frame := make([]byte, 48+4+16)
		if _, err := io.ReadFull(clientConn, frame); err != nil {
			t.Fatal(err)
		}
		if command, got := binary.BigEndian.Uint32(frame[0:4]), binary.BigEndian.Uint32(frame[4:8]); command != RetSubmit || got != sequence {
			t.Fatalf("reply %d command=%d sequence=%d, want RET_SUBMIT %d", i, command, got, sequence)
		}
		if got := frame[48:52]; !bytes.Equal(got, pcm[i*4:(i+1)*4]) {
			t.Fatalf("reply %d PCM=%v, want %v", i, got, pcm[i*4:(i+1)*4])
		}
	}
	clientConn.Close()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("USB/IP session did not close")
	}
}

type closeUnblocksWriter struct {
	once   sync.Once
	closed chan struct{}
}

func (w *closeUnblocksWriter) Write([]byte) (int, error) {
	<-w.closed
	return 0, io.ErrClosedPipe
}

func (w *closeUnblocksWriter) Close() error {
	w.once.Do(func() { close(w.closed) })
	return nil
}

func TestSessionClosesBlockedWriterBeforeWaitingForWorkers(t *testing.T) {
	device, err := uac1.NewDevice(1, 100)
	if err != nil {
		t.Fatal(err)
	}
	var wire bytes.Buffer
	req := SubmitRequest{
		Basic:                BasicHeader{Command: CmdSubmit, Sequence: 51, Direction: DirectionIn, Endpoint: 2},
		TransferBufferLength: 4,
		NumberOfPackets:      1,
		Interval:             1,
	}
	if err := binary.Write(&wire, binary.BigEndian, req); err != nil {
		t.Fatal(err)
	}
	if err := binary.Write(&wire, binary.BigEndian, IsoPacket{Length: 4}); err != nil {
		t.Fatal(err)
	}
	writer := &closeUnblocksWriter{closed: make(chan struct{})}
	server := NewServer("127.0.0.1:0", []*uac1.Device{device}, log.New(io.Discard, "", 0))
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		server.handleURBs(context.Background(), bufio.NewReader(&wire), writer, device)
	}()
	select {
	case <-finished:
	case <-time.After(time.Second):
		_ = writer.Close() // release the worker even when the assertion fails
		<-finished
		t.Fatal("session waited for a worker blocked on a non-reading client")
	}
}

func TestConnectionClosesOnServerCancellation(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()
	ctx, cancel := context.WithCancel(context.Background())
	server := NewServer("127.0.0.1:0", nil, log.New(io.Discard, "", 0))
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		server.handleConnection(ctx, serverConn)
	}()
	cancel()
	select {
	case <-finished:
	case <-time.After(time.Second):
		_ = serverConn.Close()
		<-finished
		t.Fatal("idle connection remained open after server cancellation")
	}
}

func TestDeviceListEncoding(t *testing.T) {
	dev, err := uac1.NewDevice(1, 100)
	if err != nil {
		t.Fatal(err)
	}
	s := NewServer("127.0.0.1:0", []*uac1.Device{dev}, log.New(io.Discard, "", 0))
	var b bytes.Buffer
	if err := s.writeDeviceList(&b); err != nil {
		t.Fatal(err)
	}
	// 8-byte OP header, 4-byte count, 312-byte device, 12-byte interfaces.
	if b.Len() != 336 {
		t.Fatalf("length=%d, want 336", b.Len())
	}
	if got := binary.BigEndian.Uint32(b.Bytes()[8:12]); got != 1 {
		t.Fatalf("device count=%d", got)
	}
	if !bytes.Contains(b.Bytes(), []byte("1-1")) {
		t.Fatal("bus ID missing")
	}
}
