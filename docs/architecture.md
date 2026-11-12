# Architecture

Network Statistic is a Windows desktop agent with a browser panel. The client,
HTTP server and collectors are Go. The English HTML/CSS/JavaScript panel and
public target catalogs live in `frontend/`. The English interface preserves the
HTTP routes, JSON shapes and journal formats. `panel-contract.json` records the
current English asset SHA256 and the legacy panel checksum separately, together
with HTTP routes, JSON fields, state strings, timestamps and journal formats.

## Source boundaries

There is one root module, `github.com/sa1tbaby/go-network-stat`. Source packages contain runtime
code; every `_test.go` lives under `tests/`.

```text
cmd/
  client/                   thin composition: app.HandleMode, then desktop.Run
  build/                    portable build command
  udp-echo/                 standalone console entry point
frontend/
  assets.go                 embed declarations for browser assets
  dashboard.html            English HTML/CSS/JavaScript panel
  official-relays.json      public relay catalog
  cod-test-targets.json      public diagnostic target catalog
  desktop/                  native window, tray, dialogs and supervisor
backend/
  api/                      logical API service: HTTP and panel contract
    assets/                 versioned preparation/cache of embedded frontend
    client/                 local endpoint validation and pause/control client
  networkmanager/           logical NetworkManager service: monitor and state
    collectors/             source-bound probes and native telemetry
    diagnostics/            independent network/radio/Ethernet runs
    relay/                  game identity and capture lifecycle
    routes/                 source-bound TTL route jobs
    targets/                target registry and catalogs
    storage/                measurement journals and store operations
    udpecho/                controlled UDP echo client/responder and CLI
  app/                      backend composition, lifecycle and mode dispatch
  config/                   preferences and legacy configuration import
  infrastructure/
    data/                   writable layout and default path selection
    fileutil/               atomic filesystem publication
    process/                Windows Job Object and process ownership
  contracts/                interfaces between backend components
scripts/                    optional launch/build wrappers
tools/resources/           Windows manifest generation
tests/
  unit/<component>/         pure logic and bounded component behavior
  integration/<component>/  collaborating components and local sockets
  contract/<component>/     observable panel/API compatibility
  platform/<component>/     Windows APIs, processes and UI
```

Within each service, files follow the operation rather than the transport's
size. API queries, commands, responses and static assets are separate. Application
construction, HTTP lifecycle, owner supervision and state publication are
separate. NetworkManager has dedicated files for commands, adapter refresh,
snapshots, metrics and storage history/selection/reset. Collectors separate TCP,
UDP, DNS, capture files, worker options, process identity, elevation and ETL/XML
parsing. Route jobs/probes/assessment and Ethernet/WLAN trackers stay inside their
respective domain packages.

API and NetworkManager are logical services in the same backend process.
`backend/api` owns HTTP routing, origin/endpoint checks, JSON serialization and
static panel delivery. Its `assets` package prepares the immutable frontend
cache, and its `client` package supplies local HTTP control to the desktop client.
API handlers delegate measurement operations through backend interfaces.

`backend/networkmanager` owns monitoring policy, scheduling and measurement state.
Its subpackages implement the domain operations: probes and telemetry, independent
diagnostics, game-flow detection, routes, targets, measurement persistence and
controlled UDP echo. HTTP requests do not implement those algorithms. Shared
interfaces in `backend/contracts` keep the services independent of transport
and the monitor's state lock.

`backend/app` constructs and connects API and NetworkManager, starts/stops the
server, and dispatches backend/helper command modes. `backend/config` owns
preferences and migration. `backend/infrastructure` supports the backend with
paths, atomic file operations and Windows process ownership; it contains no
presentation code.

All native presentation belongs to `frontend/desktop`: the window, tray, folder
dialog and desktop supervisor. `cmd/client` is the thin composition boundary:
it calls `app.HandleMode` before `desktop.Run` and injects native callbacks where
needed. Backend packages never import `frontend/desktop`. Embedding the immutable
`frontend` assets is separate from loading native UI code.

Collectors expose operations with an explicit source IPv4 and interface index.
Their Windows implementations call IP Helper, Winsock and Native Wi-Fi directly.
Protocol parsing, counter interpretation and UDP classification are independent
of HTTP and desktop UI. Unsupported platforms return explicit unavailability
instead of fabricated zero counters or measurements.

## Process lifecycle

The desktop process owns the window, tray and single-instance mutex. It starts
its own executable in `--backend` mode while suspended, assigns it to a
kill-on-close Windows Job Object, then resumes it. Readiness requires a unique
ready file with the expected application ID/PID and a successful `/api/status`.
The server listens on IPv4 loopback, normally port 8765, with up to 20 port attempts.
Panel commands retain the existing same-origin HTTP/JSON contract.

Closing the browser does not stop collection. Pause stops new ordinary network
probes; interface sampling and the server remain active. Diagnostic and route
jobs have their own contexts and continue independently of the ordinary monitor's
pause state. Stopping one diagnostic run does not stop the other runs.

Normal exit requests `/api/shutdown`, cancels work, writes final snapshots and
closes files. Forced desktop termination closes the Job Object and kills the
backend. Diagnostic workers are goroutines in that backend, so private
`runtime.json` records must not be interpreted as a required child-process count.

Explicit game capture starts a separate elevated Go helper through UAC. It
checks the game PID/start time, owner PID/start time, request ID and cancellation
file. It starts only its own named ETW session with the Windows `logman.exe`,
reads ETL through `wevtapi.dll`, and stops its session on completion, cancellation
or owner exit. It does not terminate games or other applications' trace sessions.
No PowerShell interpreter, C# source compilation or .NET runtime is involved.

## Writable data

`backend/infrastructure/data.Layout` defines one writable tree. The default is `data/` beside
the executable; when the executable is in `dist/`, `data/` is beside `dist/`.
Development commands started with `go run` use the working directory’s `data/`
so reports survive removal of Go’s temporary executable.
Only the default can fall back to per-user storage if it is not writable.
An explicit `--data-dir` must be an absolute writable root.

| Subdirectory | Contents |
| --- | --- |
| `config/` | Settings, selected measurement directory and custom targets. |
| `runtime/` | PID, endpoint, ready files and process state. |
| `logs/` | Service and client/backend logs. |
| `statistics/` | Default measurement store and diagnostic/route/capture history. |
| `cache/` | Versioned extracted panel/catalog assets. |
| `build/` | Temporary build files and `build-result.json`. |
| `udp/` | Standalone controlled UDP echo reports. |

An explicitly selected measurement directory remains authoritative and may be
outside this tree. Importing settings never moves history into `statistics/`.
`backend/config` reads the new configuration first and imports absent settings
from legacy `InternetStability-data`, `internet-stability` and the user's earlier
`InternetStability` directory. It also accepts an old flat explicit data directory.
Existing preferences are retained; user measurement files stay where they were.

The measurement store preserves CSV/JSONL formats and accepts legacy UTF-8 BOM.
It replays history and bounds only the recent graph window. Corrupt rows produce
a visible warning. Legacy diagnostics and new `diagnosis-runs/<id>` remain
readable. New snapshots are published through temporary-file replacement.
Switch/reset operations are serialized and rejected during active diagnostic,
route or capture work. Ownership/path checks retain unrelated files.

Immutable frontend source, release binaries in `dist/` and writable runtime data
are separate. The build command stages generated manifest resources under
`data/build/`; it does not generate `.syso` files in source packages.

## Measurement invariants

- ICMP uses `IcmpSendEcho2Ex` with the selected source address. WeakHostSend and
  current source/interface identity are checked before sending.
- TCP and UDP bind both the source IPv4 and `IP_UNICAST_IF`. A diagnostic run
  stops when its selected topology changes.
- Pending probes and local tool errors do not enter the loss denominator.
  A responding peer, a remote non-response and unavailable telemetry are distinct.
- Persistent UDP DNS sampling matches pending/expired transaction IDs. A late
  reply is reported separately and never becomes a second completed probe.
- Session p95 uses the existing 1 ms histogram; window p95 uses nearest rank.
  Monitor jitter uses adjacent successful probes and resets across non-responses.
- Wi-Fi retries are link-layer counters, not game UDP loss. Repeated PHY arrays
  are not summed. Counter resets establish a new baseline.
- Generic Ethernet counters do not expose separate CRC/FCS. Unavailable fields
  remain unknown rather than zero.
- ETW classifies a sustained bidirectional game UDP flow; it does not measure
  game packet loss. Its remote UDP port is not substituted into TCP probes.

## Validation boundary

Tests are separate blackbox packages under `tests/{unit,integration,contract,platform}`
and use public runtime APIs. Unit tests cover parsers, metrics and state machines;
integration tests cover composition, stores, cancellation and local TCP/UDP peers;
contract tests compare observable behavior with the preserved panel API contract
and verify the current English asset checksum.
Platform tests exercise Windows process ownership, UI and native collection.

Compile-time assertions validate private Windows ABI offsets/sizes during an
AMD64 cross-build. Cross-compilation does not execute WinAPI calls. Actual
multi-adapter source binding, WLAN driver support, UAC and ETW still require
runtime verification on Windows. Ordinary automated tests do not start an
elevated game capture.
