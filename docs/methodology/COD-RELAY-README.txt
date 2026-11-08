CoD and CS2 in “Game server”
===========================
The game selector changes the catalog and retains separate selections for CS2 and CoD.
CS2 retains the existing /api/official-relays and Valve appid=730 source.
CoD shows regional control targets from cod-test-targets.json. These are not presented
as Activision servers. The gateway already checked by the ordinary monitor is not added again.

“Automatically add active Relay” — CS2 and CoD
---------------------------------------------
1. Start CS2 or CoD. An active match is preferable for reliable detection.
2. Select the corresponding game and click the button.
3. Confirm the Windows administrator permission request for ETW.
4. After 20 seconds of observation, a confirmed UDP peer automatically appears
   in the ordinary ICMP cards with a route button. The monitor remains paused if it was paused.

CS2 checks the cs2.exe instance; CoD checks cod.exe. Their states, messages,
results and errors are independent. Only one capture runs at a time; switching
games does not cancel a running check or transfer its result to the other game.
CS2 peers are saved with game=cs2, origin=cs2_etw and a separate UDP port.
The existing public Valve catalog for CS2 is retained.
Additional API: POST /api/cs2/relay/start, GET /api/cs2/relay/status.
CS2 capture logs are in cs2-relay-runs/{runId}/ in the selected log folder.

Capture uses Microsoft-Windows-Kernel-Network in a separate
CodexRelay-{identifier} session. Maximum ETL size is 16 MiB; IPv4/IPv6 keywords
0x30 are enabled. The collector stops only the session it created and retries
cleanup in finally on error. Network settings, firewall and game files are not
changed. No code is injected into the game process. Network metadata is saved,
not the contents of game packets.

Parsing uses the PID from ETW event payload fields, not the header PID of incoming
events, which may belong to System. IPv4 processing handles SENDUDP 42 / RECVUDP 43;
addresses and ports are converted from network representation. The cod.exe PID
and start time are checked before capture and parsing to prevent PID reuse.

Automatic selection criteria:
- at least 80 events in each direction and 240 in total;
- at least 10 seconds of traffic;
- bidirectional traffic in at least three five-second intervals;
- the last packet is no more than three seconds old at the end of observation;
- the flow accounts for at least 60% of eligible public CoD UDP events;
- the next-largest flow is less than half the size of the selected flow.
DNS, discovery, UDP 80/443 (including QUIC content delivery) and private addresses
are excluded from automatic selection. Ambiguous or unsustained flows are not
added; the panel shows candidates and reasons.
This is a conservative heuristic: metadata cannot prove that an address is
specifically a relay rather than a game server, or calculate game UDP packet loss.
IPv6-only traffic is reported separately; existing cards and tracing are currently IPv4.

The remote UDP port is stored as UdpPort metadata and does NOT become the TCP
probe port. Detecting the same IP again updates the evidence while retaining
card history. A new peer does not remove old targets. The ordinary eight-target limit applies.

Files in the selected log folder:
cod-relay-runs/{runId}/request.json — PID, process instance and request identifier;
capture.json — stages, parsed result and candidate counters;
kernel-network.etl — bounded raw network metadata capture;
panel-result.json — automatic addition result.
data/config/custom-targets.json stores the card address and metadata.
Do not change the log folder during detection: the API blocks that operation.

API:
GET  /api/cod/test-targets
POST /api/cod/test-target/add?address={address from catalog}
POST /api/cod/relay/start
GET  /api/cod/relay/status
Viewing status and switching games do not start capture. Repeating POST during
capture returns the same runId. UAC cancellation, a closed game and errors are shown explicitly.

Implementation tests use temporary folders and check cancellation before capture
starts when the owner exits, the process instance changes or cancellation is requested.
Personal ETL files and real game-session results are not included in source control.

Implementation and data locations
---------------------------------
Coordination lives in backend/networkmanager/relay/; WinAPI/ETL and classification
live in backend/networkmanager/collectors/. Requests pass through backend/api/.
The elevated helper is --capture-worker mode of the same dist/InternetStability.exe;
it uses system logman.exe and wevtapi.dll, without PowerShell or .NET.
The measurement folder defaults to data/statistics/, or an explicit user selection.
The English panel and public catalogs live in frontend/. Interface translation
preserves the API routes, JSON shapes and journal formats. Tests live in
tests/unit/collectors/, tests/platform/collectors/ and separate relay tests under tests/.
Real elevated capture requires Windows.
