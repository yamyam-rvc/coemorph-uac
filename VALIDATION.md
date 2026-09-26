# Validation scope — not release acceptance

The local source manifest identifies the current committed USB component.
The product's pinned host binary has SHA-256
1A362267482553D1F4E91EB24514AF9C1D48C2B72808B1E69C0174684158661F.
That binary equals the separately preserved candidate binary. Its embedded
build metadata records an earlier revision plus modified=true; therefore the
current Git commit is not claimed as a bit-for-bit reproducible binary build.
A fresh build from this exact source has now completed with Go 1.26.5,
CGO_ENABLED=0, -trimpath and -buildvcs=false. Its binary SHA-256 is
6422A4A109842A58D7D0DD7595A711F810D8306B1B4D5896DB9A2CFD536B9569.
That binary has not been executed or substituted into the product. Its hash
differs from the existing product host, and the prior hardware results must
not be presented as a test of this newly built executable.

All six test-bearing packages completed successfully: 71 named tests including
subtests. The first combined invocation reached a 120-second outer deadline
after four packages passed (41 named tests). Only the two missing packages
were resumed, completing the remaining 30 tests; no test failure was observed.
The initial outer timeout remains a failure of that invocation. Separate
go vet ./... and host build commands then exited 0. These are source checks,
not driver installation or real-audio acceptance.

With signed usbip-win2 0.9.8.0, the current product research candidate completed
one owned UAC2 attach, both UI mode starts/stops, and graceful host detach.
Each mode's four-second clock probe observed 192000 frames/400 packets.
Device-QPC/host slopes were approximately 0.999969940 (low latency) and
0.999977796 (stable), with no timestamp reversal. Initial discontinuity was
present, followed by 399 unflagged packets per mode. These are clock results,
not latency values. The physical input was nearly silent and that probe did
not save PCM. It proves neither intelligible audio nor signal latency.

A prior standalone run of this pinned host also enumerated through usbaudio2,
observed a near-one capture-clock slope and detached on owner-pipe EOF.
Those short observations are not cold-start, update or long-run acceptance.

Prepared next work captures actual input and output PCM and immediate
GetBuffer receipt timestamps on the same PC QPC. No result is claimed before
that run and post-exit analysis. Endpoint device QPC is not the latency clock.
Physical pickup and full native Main/Store acceptance are separate scopes.

The UAC1 StartFrame contribution is established by original/echo/synthetic
comparisons. Direct position-query experiments showed capture timestamp
perturbations, but the complete remaining slope equation and the exact
executed transport callback counts remain unproven. UAC2 is a separate path,
not a repair claim for the original UAC1 behavior.

Formal USB identity, final notices/redistribution
audit, installation/update/cold-restart tests and final dual-mode real-input
latency remain required. Source publication or PID allocation does not change
audio behavior and cannot itself repair a clock anomaly.

Source publication under BSD-2-Clause was authorized on 2026-09-26. The
observations above retain their original candidate scope; this permission
does not constitute product release acceptance.
