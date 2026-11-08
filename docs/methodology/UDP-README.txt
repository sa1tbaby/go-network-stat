CONTROLLED UDP TEST WITHOUT A GAME
=================================

A built InternetStability.exe is required at both ends
(in the project: dist/InternetStability.exe). Run --udp-responder
on YOUR second PC or Windows VPS. --udp-test checks the handshake and does not start
a stream if the selected target has no compatible responder. Do not point the test
at a game server: CoD/CS2 are not required to answer arbitrary UDP packets.

On the second PC:
.\dist\InternetStability.exe --udp-responder -port 39091

On the PC being tested, replace 192.168.2.100 with the second PC's address:
.\dist\InternetStability.exe --udp-test -server 192.168.2.100 -adapter "Wi-Fi"

Repeat for Ethernet with -adapter "Ethernet". Use the actual adapter name on your PC.
Without -adapter, active Wi-Fi is selected, or active Ethernet if no Wi-Fi is available.
For a local network check, connecting the second PC to the router by cable is useful.
For a WAN test, run the responder on your own Windows VPS in the desired region:
this tests the path to that VPS but does not reproduce a game server or Valve SDR.

Client options:
-port 39091 by default
-pps 60 by default (range 1..120)
-payload-bytes 256 by default (32..1200 bytes)
-duration-seconds 60 by default (10..600 seconds)

Separate all options and values with a space, for example:
.\dist\InternetStability.exe --udp-test -server 192.168.2.100 -pps 60 -payload-bytes 256 -duration-seconds 60

The source IPv4 and IP_UNICAST_IF bind probes to the selected interface.
By default, the responder listens on 0.0.0.0:39091 until Ctrl+C; -bind selects
a specific IPv4 and -duration-seconds 120 limits its run time. For example: -duration-seconds 120.
It ignores incompatible packets, returns no more bytes than it received and
limits responses to 200 per second per IP. The application does not change firewall rules.
If the responder is unavailable, check inbound UDP 39091 permissions on your server.

Default logs: data/udp/udp-YYYYMMDD-HHMMSS-PID/summary.json and probes.csv.
The -logs option selects a different results folder. The shared data folder is
determined as for the main client; a separately copied EXE uses the folder beside it.
With go run, results stay in the working directory's data/, not in the temporary
Go executable directory.
Timestamps use ISO 8601 with a time zone; durations and RTT use monotonic clocks.
echoFailurePct is the fraction of completed sent probes with no valid echo within 1200 ms.
Pending probes and local send errors are not counted as lost.
If the handshake fails, the test has not started and the percentage is null, not 100%.
Echo covers the forward and return paths: upload/download loss is not known separately.
Duplicates, reordering of first responses and late replies are counted separately.
rttJitterMs is the mean absolute RTT change between adjacent timely received echoes.
It is not game UDP jitter or a one-way RFC3550 measurement.
maxClientLoopGapMs/maxSendPacingLateMs show delays on the testing PC itself.
Sleep or high Windows load can affect packet processing.
The minimum 32-byte packet is a control header: magic/version/sequence/
monotonic_timestamp/session_nonce. User files and game traffic contents are not read.

Reliable comparison requires identical parameters and conditions for Wi-Fi and Ethernet.
A clean UDP test to a LAN/VPS does not disprove a problem on a particular game route.
Observing the actual game peer/relay requires a short capture of the game connection.
After leaving the game, you can separately probe the detected IP with ICMP, but that is a different protocol.

Primary sources:
https://software.es.net/iperf/invoking.html — a UDP test requires participating client and server endpoints.
https://learn.microsoft.com/en-us/windows/win32/winsock/ipproto-ip-socket-options
https://partner.steamgames.com/doc/features/multiplayer/steamdatagramrelay?l=english
https://help.steampowered.com/en/faqs/view/5E6F-5B36-5485-F6B9

For development, a console command is available from the single Go module's root:
go run ./cmd/udp-echo responder -port 39091
go run ./cmd/udp-echo test -server 192.168.2.100 -adapter "Ethernet"
You can build a separate console EXE: go build -o dist/UDP-Echo.exe ./cmd/udp-echo
With go run, the data/ tree is created in the current directory: results survive
removal of Go's temporary executable.

Implementation: backend/networkmanager/udpecho/; console entry point: cmd/udp-echo/.
Protocol, rate limit and reports are tested in tests/unit/udpecho/
and tests/integration/udp/. Ctrl+C stops sending and waits for already sent
probes until their deadlines; pending replies do not become new sends.
