package usbip

import (
	"bytes"
	"context"
	"io"
	"log"
	"net"
	"sync"
	"testing"
	"time"

	"virtualcables/internal/uac1"
)

// Hold the final connection log to model cleanup that has not yet returned.
// Stop/restart callers must not observe Serve completion during this interval.
type connectionCleanupGate struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func TestServeListenerClosureCancelsImportedSession(t *testing.T) {
	device, err := uac1.NewDevice(1, 100)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server := NewServer("127.0.0.1:0", []*uac1.Device{device}, log.New(io.Discard, "", 0))
	finished := make(chan error, 1)
	go func() { finished <- server.Serve(ctx) }()
	select {
	case <-server.Ready():
	case <-time.After(time.Second):
		t.Fatal("startup timed out")
	}
	server.mu.Lock()
	listener := server.listener
	server.mu.Unlock()
	conn, err := net.DialTimeout("tcp", listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := writeOpHeader(conn, OpReqImport, 0); err != nil {
		t.Fatal(err)
	}
	var busID [32]byte
	copy(busID[:], device.BusID)
	if err := writeAll(conn, busID[:]); err != nil {
		t.Fatal(err)
	}
	// A complete import reply proves the connection was accepted and is live.
	reply := make([]byte, 8+312)
	if _, err := io.ReadFull(conn, reply); err != nil {
		t.Fatal(err)
	}
	_ = listener.Close()
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("listener closure left the imported session running")
	}
	var one [1]byte
	if _, err := conn.Read(one[:]); err != io.EOF {
		t.Fatalf("Serve returned without closing the imported session: %v", err)
	}
}

func (g *connectionCleanupGate) Write(p []byte) (int, error) {
	if bytes.Contains(p, []byte("management handshake")) {
		g.once.Do(func() { close(g.entered) })
		<-g.release
	}
	return len(p), nil
}

func TestServeWaitsForAcceptedConnectionCleanup(t *testing.T) {
	for _, stop := range []string{"cancel", "listener-close"} {
		t.Run(stop, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			gate := &connectionCleanupGate{entered: make(chan struct{}), release: make(chan struct{})}
			var releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(gate.release) }) }
			defer unblock()
			server := NewServer("127.0.0.1:0", nil, log.New(gate, "", 0))
			finished := make(chan error, 1)
			go func() { finished <- server.Serve(ctx) }()
			select {
			case <-server.Ready():
			case err := <-finished:
				t.Fatalf("startup failed: %v", err)
			case <-time.After(time.Second):
				t.Fatal("startup timed out")
			}
			server.mu.Lock()
			listener := server.listener
			server.mu.Unlock()
			conn, err := net.DialTimeout("tcp", listener.Addr().String(), time.Second)
			if err != nil {
				t.Fatal(err)
			}
			// EOF while reading the management handshake reaches the held cleanup.
			_ = conn.Close()
			select {
			case <-gate.entered:
			case <-time.After(time.Second):
				t.Fatal("accepted connection did not reach cleanup")
			}
			if stop == "cancel" {
				cancel()
			} else {
				_ = listener.Close()
			}
			select {
			case err := <-finished:
				t.Fatalf("Serve returned before accepted connection cleanup finished: %v", err)
			case <-time.After(100 * time.Millisecond):
			}
			unblock()
			select {
			case err := <-finished:
				if err != nil {
					t.Fatalf("shutdown failed: %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("Serve did not return after cleanup released")
			}
		})
	}
}
