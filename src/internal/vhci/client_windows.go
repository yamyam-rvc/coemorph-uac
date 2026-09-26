//go:build windows

package vhci

import (
	"context"
	"encoding/binary"
	"fmt"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

var (
	kernel      = syscall.NewLazyDLL("kernel32.dll")
	createEvent = kernel.NewProc("CreateEventW")
	getResult   = kernel.NewProc("GetOverlappedResult")
)

// Client serializes IOCTLs and Close. Callers must use cancellation/deadlines;
// cancellation requests still wait for kernel completion before freeing memory.
type Client struct {
	mu     sync.Mutex
	handle syscall.Handle
}

func Open() (*Client, error) {
	// Resolve from the actual Windows system directory, never PATH or cwd.
	var systemDir [4096]uint16
	n, _, dirErr := kernel.NewProc("GetSystemDirectoryW").Call(uintptr(unsafe.Pointer(&systemDir[0])), uintptr(len(systemDir)))
	if n == 0 || n >= uintptr(len(systemDir)) {
		return nil, fmt.Errorf("system directory: %w", dirErr)
	}
	config := syscall.NewLazyDLL(syscall.UTF16ToString(systemDir[:n]) + `\cfgmgr32.dll`)
	getSize := config.NewProc("CM_Get_Device_Interface_List_SizeW")
	getList := config.NewProc("CM_Get_Device_Interface_ListW")
	guid := syscall.GUID{Data1: 0xb4030c06, Data2: 0xdc5f, Data3: 0x4fcc, Data4: [8]byte{0x87, 0xeb, 0xe5, 0x51, 0x5a, 0x09, 0x35, 0xc0}}
	var chars uint32
	cr, _, _ := getSize.Call(uintptr(unsafe.Pointer(&chars)), uintptr(unsafe.Pointer(&guid)), 0, 0)
	if cr != 0 {
		return nil, fmt.Errorf("CM interface size: CR=%#x", cr)
	}
	if chars < 2 || chars > 65536 {
		return nil, fmt.Errorf("VHCI missing or invalid interface size %d", chars)
	}
	list := make([]uint16, chars)
	cr, _, _ = getList.Call(uintptr(unsafe.Pointer(&guid)), 0, uintptr(unsafe.Pointer(&list[0])), uintptr(chars), 0)
	if cr != 0 {
		return nil, fmt.Errorf("CM interface list: CR=%#x", cr)
	}
	var paths []string
	for start := 0; start < len(list) && list[start] != 0; {
		end := start
		for end < len(list) && list[end] != 0 {
			end++
		}
		if end == len(list) {
			return nil, fmt.Errorf("unterminated interface list")
		}
		paths = append(paths, syscall.UTF16ToString(list[start:end]))
		start = end + 1
	}
	if len(paths) != 1 {
		return nil, fmt.Errorf("expected one VHCI interface, found %d", len(paths))
	}
	path, err := syscall.UTF16PtrFromString(paths[0])
	if err != nil {
		return nil, err
	}
	h, err := syscall.CreateFile(path, syscall.GENERIC_READ|syscall.GENERIC_WRITE, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE, nil, syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL|syscall.FILE_FLAG_OVERLAPPED, 0)
	if err != nil {
		return nil, fmt.Errorf("open VHCI: %w", err)
	}
	return &Client{handle: h}, nil
}

func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.handle == syscall.InvalidHandle {
		return nil
	}
	err := syscall.CloseHandle(c.handle)
	c.handle = syscall.InvalidHandle
	return err
}

// ioctlLocked must hold mu. A fresh manual-reset event belongs to each request.
func (c *Client) ioctlLocked(ctx context.Context, code uint32, in, out []byte) (uint32, error) {
	if c.handle == syscall.InvalidHandle {
		return 0, fmt.Errorf("VHCI client is closed")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	e, _, err := createEvent.Call(0, 1, 0, 0)
	if e == 0 {
		return 0, fmt.Errorf("create IOCTL event: %w", err)
	}
	defer syscall.CloseHandle(syscall.Handle(e))
	over := &syscall.Overlapped{HEvent: syscall.Handle(e)}
	var ip, op *byte
	if len(in) > 0 {
		ip = &in[0]
	}
	if len(out) > 0 {
		op = &out[0]
	}
	var n uint32
	err = syscall.DeviceIoControl(c.handle, code, ip, uint32(len(in)), op, uint32(len(out)), &n, over)
	if err == syscall.ERROR_IO_PENDING {
		// Register after submission: an already canceled context immediately
		// cancels this submitted request, not a still-unsubmitted OVERLAPPED.
		cancelDone := make(chan struct{})
		stop := context.AfterFunc(ctx, func() { defer close(cancelDone); _ = syscall.CancelIoEx(c.handle, over) })
		ok, _, resultErr := getResult.Call(uintptr(c.handle), uintptr(unsafe.Pointer(over)), uintptr(unsafe.Pointer(&n)), 1)
		if !stop() {
			<-cancelDone
		}
		if ok == 0 {
			err = resultErr
		} else {
			err = nil
		}
	}
	// Never free buffers/OVERLAPPED just because CancelIoEx returned.
	runtime.KeepAlive(in)
	runtime.KeepAlive(out)
	runtime.KeepAlive(over)
	if err != nil {
		if err == syscall.ERROR_OPERATION_ABORTED && ctx.Err() != nil {
			return 0, ctx.Err()
		}
		return 0, fmt.Errorf("VHCI IOCTL %#x: %w", code, err)
	}
	if uint64(n) > uint64(len(out)) {
		return 0, fmt.Errorf("driver returned oversized output %d", n)
	}
	return n, nil
}

func (c *Client) listLocked(ctx context.Context) ([]Device, error) {
	// Bounded allocation, no retry loop or implicit driver mutations.
	b := make([]byte, 4+256*deviceSize)
	binary.LittleEndian.PutUint32(b, querySize)
	n, err := c.ioctlLocked(ctx, getDevices, b[:4], b)
	if err != nil {
		return nil, err
	}
	return parseDevices(b[:n])
}

func (c *Client) List(ctx context.Context) ([]Device, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.listLocked(ctx)
}

// AttachLocalOnce attempts one connection to localhost:3240. An error can leave
// state uncertain; callers must inspect List before retrying. No automatic retry.
func (c *Client) AttachLocalOnce(ctx context.Context, busID string, lowLatency bool) (uint32, error) {
	b, err := attachRequest(busID, lowLatency)
	if err != nil {
		return 0, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	devices, err := c.listLocked(ctx)
	if err != nil {
		return 0, err
	}
	for _, d := range devices {
		if d.Host == "127.0.0.1" && d.Service == "3240" && d.BusID == busID {
			return 0, fmt.Errorf("bus %s is already attached on port %d", busID, d.Port)
		}
	}
	n, err := c.ioctlLocked(ctx, attachOnce, b, b[:8])
	if err != nil {
		return 0, err
	}
	return parseAttachReply(b[:n])
}

// DetachVerified never sends the all-ports sentinel. It rechecks the entire
// expected snapshot immediately before detaching the explicitly supplied port.
func (c *Client) DetachVerified(ctx context.Context, expected Device) error {
	if !expected.IsLocalLabCable() {
		return fmt.Errorf("refusing to detach a non-lab device")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	devices, err := c.listLocked(ctx)
	if err != nil {
		return err
	}
	found := false
	for _, d := range devices {
		if d.Port == expected.Port {
			if d != expected {
				return fmt.Errorf("port identity changed")
			}
			found = true
		}
	}
	if !found {
		return fmt.Errorf("expected port is no longer attached")
	}
	b := make([]byte, 8)
	binary.LittleEndian.PutUint32(b, 8)
	binary.LittleEndian.PutUint32(b[4:], expected.Port)
	_, err = c.ioctlLocked(ctx, detachPort, b, nil)
	return err
}
