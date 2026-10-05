# Coemorph USB Audio

The Coemorph USB/UAC component and its modifications are available under
BSD-2-Clause. Original upstream notices are retained. The separate
voice-conversion application, model weights and purchased content are outside
this repository.

## Current allocated-ID source

[pid.codes allocates 1209:C0E0 to Coemorph](https://pid.codes/1209/C0E0/).
The [accepted request](https://github.com/pidcodes/pidcodes.github.com/pull/1287)
covers this open-source software USB implementation. Installations retain their
distinct persistent serials. Allocation does not establish USB-IF certification.

The corresponding source for the current component host is
[Coemorph_USB_Audio_Source_1209_C0E0.zip](Coemorph_USB_Audio_Source_1209_C0E0.zip).
It is the byte-identical source archive included with the engineering candidate:

* Source archive SHA-256: `E004546BBDD4359071C3040839E67BCE4607F351DEEDF6EF8A68C44DCC164F62`
* Component host SHA-256: `06E1F24A04DB0796A0558815D089DFA05AF5AA8B73394DC75B296E29E1AB73AA`

[CURRENT_COMPONENT_SOURCE.json](CURRENT_COMPONENT_SOURCE.json) identifies this
snapshot. The archive contains 43 source/test files, retained license notices,
build instructions, a source hash manifest and component validation metadata.
The `src` tree and older root validation records remain the historical
[pre-allocation snapshot](https://github.com/yamyam-rvc/coemorph-uac/tree/4562aada25ea3c99d42197d55ddd4d9628ede3c9).
Use the allocated-ID archive for the current component.

## Build and ownership

Extract the archive and, on Windows with Go 1.26.5 and `CGO_ENABLED=0`, run from
its `src` directory:

```text
go test ./...
go vet ./...
go build -trimpath -buildvcs=false -o CoemorphUACHost.exe ./cmd/coemorph-uac-host
```

The host exports a 48 kHz stereo, 16-bit PCM UAC2 playback/capture pair named
Coemorph through localhost USB/IP. It uses an already installed signed
usbip-win2 transport and the Windows USB Audio 2.0 class driver.
Its owner supplies redirected stdin as a lifetime lease and an explicit serial:
`-serial yamyam.rvc@gmail.com:COEMORPH-<32 uppercase hexadecimal digits>-001`.
Pipe EOF initiates owned-device detach and server drain.

The USB implementation derives from
[Virtual-Cables](https://github.com/tarekwasfy01/Virtual-Cables) at
`5c0a4f75d41a85691912f0640ecac26045e923ed`. The native transport client uses the
public usbip-win2 v.0.9.8.0 ABI; its BSD notice and the Go notice are included.
[LICENSE.Go.txt](LICENSE.Go.txt) retains the additional Go runtime
`memmove_amd64.s` notices accompanying the product host. The source-only archive
retains the general Go license; its bytes have been preserved.
This source component has no third-party Go module dependencies.

## Validation scope

The retained allocated-ID build passed six test-bearing packages (89 named
test/subtest pass events), `go vet` and host compilation. An initial test command
exceeded its outer deadline; a bounded subsequent package run passed. The timeout
remains part of the internal execution record.

Component test/build evidence covers the identified host. Final product
acceptance, default-device protection, endpoint numbering, installation/update,
Store acceptance and physical-input-to-virtual-microphone latency remain subject
to their product checks. The earlier UAC1 timestamp mechanism remains unresolved.
Historical lab entry points retain their experimental identities.

This publication contains USB component source and notices. Driver/installer
binaries, voice-conversion code, model weights, customer data and VC
redistributables are excluded. The two `.bin` archive members are public-ABI
unit-test fixtures.
