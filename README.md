# Network Statistic

A portable Windows application for checking Wi-Fi and Ethernet connection stability.
The client, server and collectors are written in Go; the panel runs in a regular browser.
The English HTML/CSS/JavaScript interface preserves the HTTP routes, JSON shapes
and journal formats. Running the EXE requires no Go, PowerShell or .NET installation.

The application collects ICMP/TCP responses, RTT/p95/jitter and interface state,
checks routes, runs independent two-minute ICMP/UDP DNS diagnostics, reads
Wi-Fi/Ethernet counters and identifies the active CoD/CS2 UDP peer.
ICMP nonresponses and Wi-Fi retries do not represent game UDP packet loss.

## Build and run

Go 1.24+ is required. Run all commands from the repository root:

```sh
go run ./cmd/build
```

The output is `dist/InternetStability.exe` for Windows x64. The command runs tests,
generates a Windows manifest without a C compiler and builds the EXE. Building
is supported on Windows and Linux; no third-party Go modules or separate runtimes
are required. On Windows, you can use the wrapper:

```powershell
.\scripts\build.ps1 -Go 'C:\Tools\Go\bin\go.exe'
```

Open the EXE, then click “Open dashboard”. The usual address is
`http://127.0.0.1:8765/`; if the port is occupied, the next available port is used.
The close button hides the window in the tray. “Exit — stop everything” stops
the server and checks. Closing the browser does not stop measurements. More instructions:
[PORTABLE-CLIENT.txt](PORTABLE-CLIENT.txt), [BUILDING.txt](docs/BUILDING.txt).

## Structure

There is one root `go.mod`; the module is `github.com/sa1tbaby/go-network-stat`.

| Directory | Responsibility |
| --- | --- |
| `frontend/` | Browser panel, two JSON catalogs of public targets and `assets.go` for embedding. |
| `frontend/desktop/` | Native Windows window, tray, folder picker and client supervisor. |
| `backend/api/` | API service: HTTP routes, panel JSON contract, asset cache preparation and client HTTP control. |
| `backend/networkmanager/` | NetworkManager service: measurement scheduling and state. |
| `backend/networkmanager/{collectors,diagnostics,relay,routes,targets,storage,udpecho}/` | Native probes and telemetry, independent checks, game UDP, routes, targets, history and controlled UDP echo. |
| `backend/app/` | Backend service composition, lifecycle and server/worker mode dispatch. |
| `backend/config/` | Application settings and legacy configuration import. |
| `backend/infrastructure/{data,fileutil,process}/` | Shared data layout, atomic writes and Windows process ownership. |
| `backend/contracts/` | Interfaces between backend components. |
| `cmd/client/`, `cmd/build/`, `cmd/udp-echo/` | Thin client entry point, build command and console UDP utility. |
| `tools/resources/`, `scripts/` | Manifest generation and optional launch/build wrappers. |
| `tests/` | All tests: `unit/`, `integration/`, `contract/`, `platform/`, then the component name. |

API and NetworkManager run in one backend process and are separated into Go packages.
API handles HTTP and panel response formats; NetworkManager handles measurements,
their state and results. Shared infrastructure supports both services.
The native interface lives entirely in `frontend/desktop/`: `cmd/client` first
passes worker modes to `backend/app`, then starts the desktop UI. The folder picker
is injected into the backend as a callback; the backend does not import desktop UI.

Boundaries and lifecycle are described in the [architecture](docs/architecture.md).
The [panel contract](docs/panel-contract.json) records the API, JSON fields, journal
formats and SHA256 of the current English `frontend/dashboard.html` asset.
The legacy panel checksum is recorded separately for historical reference.

## Data

By default, the application creates one `data/` tree beside the EXE. For an EXE
in `dist/`, it uses the adjacent `data/` in the project root. When started with
`go run`, data stays in the working directory's `data/`:

```text
data/
  config/       settings and custom targets
  runtime/      PID, dashboard address, ready/state files
  logs/         application service logs
  statistics/   ordinary measurements and check history
  cache/        extracted immutable panel assets
  build/        temporary build files and build-result.json
  udp/          standalone UDP echo test results
```

If the default location is not writable, the per-user data directory is used.
`--data-dir` specifies an absolute path to the root of this tree. The history
folder can be selected separately in the window or panel; an explicit selection
is saved and takes precedence over `data/statistics/`.

Settings are imported from the former `InternetStability-data`, `internet-stability`
and `%APPDATA%\InternetStability` locations. History is neither moved nor reset:
select an existing CSV/JSONL folder to continue recording there. Folder switching
and reset are blocked while diagnostics, routes or ETW are active. Unrelated files are retained.

## Validation

```sh
go test ./...
go vet ./...
```

All `_test.go` files live in `tests/` and access packages through their production APIs.
Integration TCP/UDP tests use temporary folders and local sockets.
Windows window, process and native collector tests run on Windows:

```powershell
go test ./tests/platform/...
```

Cross-compilation checks native code compilation and ABI structure sizes.
Actual adapters, WLAN, UAC and ETW require verification on Windows;
a Windows cross-build on Linux does not replace that verification. Ordinary
automated tests do not start a real elevated game capture.

Methodology and limitations are in [docs/methodology/](docs/methodology/README.txt).
The standalone UDP echo test is described in [UDP-README.txt](docs/methodology/UDP-README.txt).

## License

[MIT](LICENSE).
