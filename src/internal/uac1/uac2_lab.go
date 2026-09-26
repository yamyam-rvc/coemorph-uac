package uac1

import "encoding/binary"

// NewUAC2LabDevice is an isolated full-speed UAC2 experiment. It shares the
// existing PCM ring and USB/IP transfer implementation; it is not the default
// product device. VID/PID remain experimental and must not be distributed.
func NewUAC2LabDevice(serial string) (*Device, error) {
	return NewUAC2DeviceWithSerial(1, 250, serial)
}

// NewUAC2DeviceWithSerial builds the tested UAC2 topology with an explicit
// installation identity. It still uses an experimental USB ID, not a release ID.
func NewUAC2DeviceWithSerial(number, latencyMS int, serial string) (*Device, error) {
	d, err := NewDeviceWithSerial(number, latencyMS, serial)
	if err != nil {
		return nil, err
	}
	d.audioClass2 = true
	binary.LittleEndian.PutUint16(d.Descriptors.Device[2:4], 0x0200)
	d.Descriptors.Device[4], d.Descriptors.Device[5], d.Descriptors.Device[6] = 0xef, 2, 1
	binary.LittleEndian.PutUint16(d.Descriptors.Device[12:14], 0x0200)
	d.Descriptors.Config = uac2Configuration()
	return d, d.Descriptors.Validate()
}

func uac2Configuration() []byte {
	var b []byte
	add := func(v ...byte) { b = append(b, v...) }
	add(9, DescriptorConfiguration, 0, 0, 3, 1, 0, 0x80, 50)
	// IAD: one audio function containing AC and both AS interfaces.
	add(8, 0x0b, 0, 3, 1, 0, 0x20, 2)
	add(9, DescriptorInterface, 0, 0, 0, 1, 1, 0x20, 0)
	// ADC 2.0, I/O box, 75 bytes of AC class descriptors, no latency control.
	add(9, DescriptorCSInterface, 1, 0, 2, 8, 75, 0, 0)
	// Clock 10: fixed 48 kHz, synchronous to USB SOF. Frequency and validity
	// are read-only. The existing transfer scheduler supplies 48 frames/ms.
	add(8, DescriptorCSInterface, 0x0a, 10, 5, 5, 0, 0)
	// USB input 1 -> speaker 3; microphone 4 -> USB output 6, same clock.
	// No feature unit: the inherited UAC1 mute/volume variables do not alter PCM.
	add(17, DescriptorCSInterface, 2, 1, 1, 1, 0, 10, 2, 3, 0, 0, 0, 0, 0, 0, 0)
	add(12, DescriptorCSInterface, 3, 3, 1, 3, 0, 1, 10, 0, 0, 2)
	add(17, DescriptorCSInterface, 2, 4, 1, 2, 0, 10, 2, 3, 0, 0, 0, 0, 0, 0, 2)
	add(12, DescriptorCSInterface, 3, 6, 1, 1, 0, 4, 10, 0, 0, 0)
	for _, stream := range []struct{ iface, terminal, endpoint, attributes byte }{
		{1, 1, 0x01, 0x09}, {2, 6, 0x82, 0x0d},
	} {
		add(9, DescriptorInterface, stream.iface, 0, 0, 1, 2, 0x20, 0)
		add(9, DescriptorInterface, stream.iface, 1, 1, 1, 2, 0x20, 0)
		add(16, DescriptorCSInterface, 1, stream.terminal, 0, 1, 1, 0, 0, 0, 2, 3, 0, 0, 0, 0)
		add(6, DescriptorCSInterface, 2, 1, 2, 16)
		add(7, DescriptorEndpoint, stream.endpoint, stream.attributes, 192, 0, 1)
		add(8, DescriptorCSEndpoint, 1, 0, 0, 0, 0, 0)
	}
	binary.LittleEndian.PutUint16(b[2:4], uint16(len(b)))
	return b
}

// Caller holds d.mu. UAC2 uses CUR/RANGE with direction in bmRequestType,
// unlike UAC1's GET_CUR request code. Unknown or unsupported controls stall.
func (d *Device) handleUAC2Control(s SetupPacket) ([]byte, int32) {
	if s.RequestType != 0xa1 || s.Index != 0x0a00 || byte(s.Value) != 0 {
		return nil, -32
	}
	switch s.Value >> 8 {
	case 1: // Clock sampling frequency.
		switch s.Request {
		case 1:
			b := make([]byte, 4)
			binary.LittleEndian.PutUint32(b, 48000)
			return truncate(b, s.Length), 0
		case 2:
			b := make([]byte, 14)
			binary.LittleEndian.PutUint16(b, 1)
			binary.LittleEndian.PutUint32(b[2:], 48000)
			binary.LittleEndian.PutUint32(b[6:], 48000)
			// Fixed frequency: minimum == maximum, resolution == 0.
			return truncate(b, s.Length), 0
		}
	case 2: // Clock validity.
		if s.Request == 1 {
			return truncate([]byte{1}, s.Length), 0
		}
	}
	return nil, -32
}

// InterfaceTuples describes alternate zero once per interface for USB/IP
// management replies. The configuration, not a hardcoded UAC1 protocol, owns it.
func (d *Descriptors) InterfaceTuples() [][4]byte {
	var result [][4]byte
	for pos := 0; pos < len(d.Config); {
		n := int(d.Config[pos])
		if n < 2 || pos+n > len(d.Config) {
			break
		}
		v := d.Config[pos : pos+n]
		if v[1] == DescriptorInterface && n >= 9 && v[3] == 0 {
			result = append(result, [4]byte{v[5], v[6], v[7], 0})
		}
		pos += n
	}
	return result
}
