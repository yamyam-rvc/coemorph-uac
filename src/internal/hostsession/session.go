// Package hostsession owns one localhost cable attachment and its server.
// It does not install drivers or adopt attachments left by another process.
package hostsession

import (
	"context"
	"errors"
	"fmt"
	"time"

	"virtualcables/internal/vhci"
)

type Transport interface {
	List(context.Context) ([]vhci.Device, error)
	AttachLocalOnce(context.Context, string, bool) (uint32, error)
	DetachVerified(context.Context, vhci.Device) error
	Close() error
}

type Server interface {
	Ready() <-chan struct{}
	Serve(context.Context) error
}

func localBus(d vhci.Device) bool {
	return d.Host == "127.0.0.1" && d.Service == "3240" && d.BusID == "1-1"
}

func operationError(ctx context.Context, err error) error {
	if ctx.Err() != nil && errors.Is(err, ctx.Err()) {
		return nil
	}
	return err
}

// Run owns transport, including on failure. The listener must be exclusively
// bound before the first driver operation, and stays bound through detach.
// attached reports driver attachment only, NOT PnP endpoint/audio readiness.
// Cancellation is a normal owner shutdown. Driver/server/cleanup errors remain
// errors. There is no force-close or successful exit before server drain.
func Run(ctx context.Context, transport Transport, server Server, attached func(vhci.Device) error) (result error) {
	defer func() { result = errors.Join(result, transport.Close()) }()
	if ctx.Err() != nil {
		return nil
	}
	serveCtx, stopServer := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.Serve(serveCtx) }()
	consumed := false
	defer func() {
		stopServer()
		if !consumed {
			result = errors.Join(result, <-done)
		}
	}()
	select {
	case err := <-done:
		consumed = true
		if err == nil {
			err = errors.New("server exited before listener readiness")
		}
		return err
	case <-ctx.Done():
		return nil
	case <-server.Ready():
	}
	if ctx.Err() != nil {
		return nil
	}
	devices, err := transport.List(ctx)
	if err != nil {
		return operationError(ctx, err)
	}
	for _, d := range devices {
		if localBus(d) {
			return fmt.Errorf("local cable already attached on port %d; refusing adoption", d.Port)
		}
	}
	if ctx.Err() != nil {
		return nil
	}
	// Our exclusively bound listener plus empty bus preflight establishes the
	// attempt boundary. Even an interrupted IOCTL may have imported this cable.
	var owned *vhci.Device
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer cancel()
		if owned == nil {
			current, listErr := transport.List(cleanupCtx)
			if listErr != nil {
				result = errors.Join(result, fmt.Errorf("reconcile uncertain attach: %w", listErr))
				return
			}
			var matches []vhci.Device
			for _, d := range current {
				if localBus(d) {
					matches = append(matches, d)
				}
			}
			if len(matches) == 0 {
				return
			}
			if len(matches) != 1 || !matches[0].IsLocalLabCable() || matches[0].SerialOverride != "" || matches[0].LowLatency {
				result = errors.Join(result, errors.New("uncertain attach identity; no detach attempted"))
				return
			}
			owned = &matches[0]
		}
		if owned == nil {
			return
		}
		if err := transport.DetachVerified(cleanupCtx, *owned); err != nil {
			result = errors.Join(result, fmt.Errorf("detach owned cable: %w", err))
			return
		}
		after, err := transport.List(cleanupCtx)
		if err != nil {
			result = errors.Join(result, fmt.Errorf("verify detach: %w", err))
			return
		}
		for _, d := range after {
			if localBus(d) {
				result = errors.Join(result, errors.New("local cable remains after detach"))
				break
			}
		}
	}()
	port, err := transport.AttachLocalOnce(ctx, "1-1", false)
	if err != nil {
		return operationError(ctx, fmt.Errorf("attach: %w", err))
	}
	devices, err = transport.List(ctx)
	if err != nil {
		return operationError(ctx, fmt.Errorf("verify attach: %w", err))
	}
	for _, d := range devices {
		if d.Port == port && localBus(d) && d.IsLocalLabCable() && d.SerialOverride == "" && !d.LowLatency {
			copy := d
			owned = &copy
			break
		}
	}
	if owned == nil {
		return errors.New("attached port identity not verified")
	}
	if ctx.Err() != nil {
		return nil
	}
	if attached != nil {
		if err := attached(*owned); err != nil {
			return err
		}
	}
	select {
	case <-ctx.Done():
		return nil
	case err := <-done:
		consumed = true
		if err == nil {
			err = errors.New("server exited while owner was active")
		}
		return err
	}
}
