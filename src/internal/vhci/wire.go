// Package vhci controls an already installed usbip-win2 driver. It does not
// install drivers, change security, or register persistent attachments.
// ABI derived from usbip-win2 v.0.9.8.0, BSD-2-Clause; see LICENSE.usbip-win2.txt.
package vhci

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strconv"
)

const (
	getDevices = 0x22e008
	attachOnce = 0x22e018
	detachPort = 0x22e004
	deviceSize = 1128
	querySize  = 1132
	attachSize = 1120
)

// Device is a snapshot from the driver, not proof of endpoint/audio readiness.
type Device struct {
	Port           uint32 `json:"port"`
	BusID          string `json:"bus_id"`
	Host           string `json:"host"`
	Service        string `json:"service"`
	Vendor         uint16 `json:"vendor"`
	Product        uint16 `json:"product"`
	SerialOverride string `json:"serial_override"`
	LowLatency     bool   `json:"low_latency"`
}

func validBusID(busID string) bool {
	for i := 1; i <= 32; i++ {
		if busID == "1-"+strconv.Itoa(i) {
			return true
		}
	}
	return false
}

func attachRequest(busID string, lowLatency bool) ([]byte, error) {
	if !validBusID(busID) {
		return nil, fmt.Errorf("invalid local cable bus ID %q", busID)
	}
	b := make([]byte, attachSize)
	binary.LittleEndian.PutUint32(b, attachSize)
	copy(b[8:40], busID)
	copy(b[40:72], "3240")
	copy(b[72:1097], "127.0.0.1")
	// Empty serial override preserves the descriptor's installation identity.
	if lowLatency {
		b[1116] = 1
	}
	return b, nil
}

func parseAttachReply(b []byte) (uint32, error) {
	if len(b) != 8 || binary.LittleEndian.Uint32(b) != attachSize {
		return 0, fmt.Errorf("invalid attach reply")
	}
	port := binary.LittleEndian.Uint32(b[4:8])
	if port == 0 || port > 0x7fffffff {
		return 0, fmt.Errorf("invalid attached port %d", port)
	}
	return port, nil
}

func parseDevices(b []byte) ([]Device, error) {
	if len(b) < 4 || binary.LittleEndian.Uint32(b) != querySize || (len(b)-4)%deviceSize != 0 {
		return nil, fmt.Errorf("invalid driver device-list reply")
	}
	devices := make([]Device, 0, (len(b)-4)/deviceSize)
	ports := make(map[uint32]bool)
	for offset := 4; offset < len(b); offset += deviceSize {
		r := b[offset : offset+deviceSize]
		port := binary.LittleEndian.Uint32(r)
		if port == 0 || port > 0x7fffffff || ports[port] || r[1125] > 1 {
			return nil, fmt.Errorf("invalid/duplicate device record")
		}
		ports[port] = true
		fields := []string{}
		for _, span := range [][2]int{{4, 36}, {36, 68}, {68, 1093}, {1108, 1124}} {
			v := r[span[0]:span[1]]
			end := bytes.IndexByte(v, 0)
			if end < 0 {
				return nil, fmt.Errorf("unterminated device string")
			}
			fields = append(fields, string(v[:end]))
		}
		devices = append(devices, Device{Port: port, BusID: fields[0], Service: fields[1], Host: fields[2], SerialOverride: fields[3], Vendor: binary.LittleEndian.Uint16(r[1104:1106]), Product: binary.LittleEndian.Uint16(r[1106:1108]), LowLatency: r[1125] != 0})
	}
	return devices, nil
}

// IsLocalLabCable is deliberately restricted to the current experimental ID.
// Official allocated IDs must be explicitly bound before product adoption.
func (d Device) IsLocalLabCable() bool {
	return d.Port > 0 && d.Port <= 0x7fffffff && d.Host == "127.0.0.1" && d.Service == "3240" && validBusID(d.BusID) && d.Vendor == 0xffff && d.Product == 0xca01
}
