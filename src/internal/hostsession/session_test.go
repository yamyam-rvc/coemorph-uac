package hostsession

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"virtualcables/internal/vhci"
)

type trace struct {
	sync.Mutex
	events []string
}

func (t *trace) add(v string) { t.Lock(); defer t.Unlock(); t.events = append(t.events, v) }

type fakeServer struct {
	trace *trace
	ready chan struct{}
	fail  error
}

func (s *fakeServer) Ready() <-chan struct{} { return s.ready }
func (s *fakeServer) Serve(ctx context.Context) error {
	if s.fail != nil {
		return s.fail
	}
	s.trace.add("bound")
	close(s.ready)
	<-ctx.Done()
	s.trace.add("drained")
	return nil
}
func cable() vhci.Device {
	return vhci.Device{Port: 3, BusID: "1-1", Host: "127.0.0.1", Service: "3240", Vendor: 0xffff, Product: 0xca01}
}

type fakeTransport struct {
	trace                    *trace
	devices                  []vhci.Device
	attachError, detachError error
	attachCancel             context.CancelFunc
	foreignAfterAttach       bool
}

func (c *fakeTransport) List(context.Context) ([]vhci.Device, error) {
	c.trace.add("list")
	return append([]vhci.Device(nil), c.devices...), nil
}
func (c *fakeTransport) AttachLocalOnce(_ context.Context, bus string, low bool) (uint32, error) {
	c.trace.add("attach")
	if bus != "1-1" || low {
		return 0, errors.New("wrong attach arguments")
	}
	d := cable()
	if c.foreignAfterAttach {
		d.Vendor = 0x1234
	}
	c.devices = append(c.devices, d)
	if c.attachCancel != nil {
		c.attachCancel()
	}
	return d.Port, c.attachError
}
func (c *fakeTransport) DetachVerified(_ context.Context, d vhci.Device) error {
	c.trace.add("detach")
	if c.detachError != nil {
		return c.detachError
	}
	if len(c.devices) != 1 || c.devices[0] != d {
		return errors.New("identity changed")
	}
	c.devices = nil
	return nil
}
func (c *fakeTransport) Close() error { c.trace.add("closed"); return nil }
func fixtures() (*trace, *fakeServer, *fakeTransport) {
	tr := &trace{}
	return tr, &fakeServer{trace: tr, ready: make(chan struct{})}, &fakeTransport{trace: tr}
}

func TestOwnerStopDetachesBeforeServerDrain(t *testing.T) {
	tr, s, c := fixtures()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err := Run(ctx, c, s, func(d vhci.Device) error {
		if d != cable() {
			return errors.New("wrong ready identity")
		}
		tr.add("attached")
		cancel()
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"bound", "list", "attach", "list", "attached", "detach", "list", "drained", "closed"}
	if !reflect.DeepEqual(tr.events, want) {
		t.Fatalf("order %v want %v", tr.events, want)
	}
}

func TestBindFailureDoesNotTouchAttachments(t *testing.T) {
	tr, s, c := fixtures()
	s.fail = errors.New("address in use")
	if err := Run(context.Background(), c, s, nil); err == nil || !strings.Contains(err.Error(), "address in use") {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(tr.events, []string{"closed"}) {
		t.Fatal(tr.events)
	}
}

func TestExistingAttachmentIsNeverAdoptedOrDetached(t *testing.T) {
	tr, s, c := fixtures()
	c.devices = []vhci.Device{cable()}
	if err := Run(context.Background(), c, s, nil); err == nil || !strings.Contains(err.Error(), "refusing adoption") {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(tr.events, []string{"bound", "list", "drained", "closed"}) {
		t.Fatal(tr.events)
	}
	if len(c.devices) != 1 {
		t.Fatal("preexisting device changed")
	}
}

func TestInterruptedAttachReconcilesAndCleansOnlyOwnedCable(t *testing.T) {
	tr, s, c := fixtures()
	c.attachError = errors.New("interrupted reply")
	err := Run(context.Background(), c, s, nil)
	if err == nil || !strings.Contains(err.Error(), "interrupted reply") {
		t.Fatal(err)
	}
	want := []string{"bound", "list", "attach", "list", "detach", "list", "drained", "closed"}
	if !reflect.DeepEqual(tr.events, want) || len(c.devices) != 0 {
		t.Fatal(tr.events)
	}
}

func TestCancellationDuringAttachStillCleansUp(t *testing.T) {
	_, s, c := fixtures()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.attachCancel = cancel
	c.attachError = context.Canceled
	if err := Run(ctx, c, s, nil); err != nil {
		t.Fatal(err)
	}
	if len(c.devices) != 0 {
		t.Fatal("canceled attach left a device")
	}
}

func TestForeignReconciliationFailsWithoutDetach(t *testing.T) {
	tr, s, c := fixtures()
	c.attachError = errors.New("interrupted reply")
	c.foreignAfterAttach = true
	err := Run(context.Background(), c, s, nil)
	if err == nil || !strings.Contains(err.Error(), "uncertain attach identity") {
		t.Fatal(err)
	}
	for _, v := range tr.events {
		if v == "detach" {
			t.Fatal("foreign device detached")
		}
	}
}

func TestDetachFailureCannotBecomeSuccess(t *testing.T) {
	_, s, c := fixtures()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.detachError = errors.New("driver detach failure")
	err := Run(ctx, c, s, func(vhci.Device) error { cancel(); return nil })
	if err == nil || !strings.Contains(err.Error(), "driver detach failure") {
		t.Fatal(err)
	}
}

func TestOwnerGoneBeforeStartDoesNotBindOrAttach(t *testing.T) {
	tr, s, c := fixtures()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Run(ctx, c, s, nil); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(tr.events, []string{"closed"}) {
		t.Fatal(tr.events)
	}
}
