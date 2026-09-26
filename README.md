# Coemorph USB Audio

This USB component, including the Coemorph modifications, is published under
the BSD-2-Clause license with the owner's authorization. Existing upstream
copyright notices remain unchanged. Source publication is not product release
acceptance; the validation limits below still apply.

The current Windows host (`src/cmd/coemorph-uac-host`) exports a software USB
Audio Class 2.0 playback/capture device named こえもーふ through localhost USB/IP.
It uses stereo 48 kHz, 16-bit PCM and a persistent installation serial.
It needs the separately installed, signed usbip-win2 transport and Windows'
USB audio class driver. This source does not contain a kernel driver.

The USB implementation derives from tarekwasfy01/Virtual-Cables at commit
5c0a4f75d41a85691912f0640ecac26045e923ed. The native client uses the public
usbip-win2 v.0.9.8.0 ABI, with its original BSD notice retained. See LICENSE,
LICENSE.usbip-win2.txt and SOURCE_MANIFEST.json. LICENSE.Go.txt accompanies the
compiler/runtime licensing information; no Go runtime binary is bundled here.

## Build and ownership

On Windows with Go 1.26.5, from `src`:

    go test ./...
    go vet ./...
    go build -trimpath -buildvcs=false -o CoemorphUACHost.exe ./cmd/coemorph-uac-host

The module has no third-party Go module dependencies and uses CGO_ENABLED=0.
All six test-bearing packages passed (71 named tests including subtests),
followed by go vet and this host build. The first all-package invocation hit
a 120-second outer deadline after four packages passed; only the remaining
two were resumed. The initial timeout is preserved, not relabeled as exit 0.
The resulting executable was not run. See VALIDATION.md and BUILD_VALIDATION.json.

The host requires redirected stdin as its ownership lease and an explicit
`-serial COEMORPH-<32 uppercase hexadecimal digits>-001`. Closing the owner pipe
initiates detach of the owned device and drains the USB/IP server. It is a
component for a supervisor, not an installer or a console-only application.
Lifecycle errors must be reported by the supervising application.

The source tree also retains the earlier UAC1 GUI and diagnostic commands for
comparison. They are not the current Coemorph product integration. The package
name `internal/uac1` is historical; the UAC2 constructor is in `uac2_lab.go`.
The native VHCI client avoids the upstream VC-dependent user-space clients,
but a signed transport still needs to be installed separately.

## Experimental identity and validation limits

FFFF:CA01 is an experimental placeholder, not an allocated Coemorph USB ID.
Do not ship that identity. A proposed pid.codes request is separate; no PID
has been assigned to this project and no USB-IF endorsement is claimed.

A locally added synthetic USB/IP StartFrame override previously contributed
approximately +1.0 to the measured UAC1 capture-clock slope. That override was
removed. The remaining UAC1 timing behavior depends on the configuration;
the complete mechanism and executed callback counts are not established.
It is not attributed here to a specific driver defect.

The current UAC2 path avoids the observed UAC1 timestamp symptom in limited
short runs. This is not proof that the original UAC1 mechanism was repaired,
nor a guarantee covering every Windows environment. Product PCM signal and
final latency, cold restart, installer/update and Store acceptance remain
unfinished. UAC2 does not advertise the inherited UAC1 mute/volume controls
which do not alter PCM.

This repository contains USB-component source/tests and licensing documents only.
It excludes Coemorph voice-conversion code, model weights, purchased content,
customer data, driver/installer binaries and VC redistributables. The two
small `.bin` files are public-ABI unit-test fixtures, not executable binaries.
This USB repository is independently open source; that does not
make the separate voice-conversion application or its models open source.
