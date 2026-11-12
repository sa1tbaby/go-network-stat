CONNECTION STABILITY TEST
=========================

Launch: double-click dist/InternetStability.exe or scripts/windows/Start-Stability.cmd.
A separately copied EXE also runs without installation.
A small Go client window opens; “Open dashboard” opens the browser.
The client controls the server and all collectors. Launching it again restores its window.
Pause: click “Stop” in the panel or run scripts/windows/Stop-Stability.cmd. “Start” resumes collection in the same log; the panel stays available. While paused, no new network probes are sent, but adapter state is still recorded.
Closing the tab does not stop collection. Minimizing or closing the client window
hides it in the tray. To exit completely: right-click the icon → “Exit — stop everything”.
Sleep may cause a gap; after restarting Windows, launch the test again. No Windows startup entry is added.

Dashboard address: recorded in data/runtime/run-state.json, usually http://127.0.0.1:8765/.
If the port is occupied, the next available port is used. The server accepts only local connections.

What is measured
----------------
• ICMP Echo to your router: checks the local PC ↔ router path.
• ICMP Echo to 1.1.1.1 and 8.8.8.8: checks responses from external nodes.
• TCP to 1.1.1.1:443 every 5 seconds: a separate connection establishment check.
• Adapter state and hardware counters every 0.5 seconds.

Wi-Fi and Ethernet are checked separately. Connect Ethernet: once it has IPv4 and a gateway,
measurements start automatically, even if Wi-Fi is also connected.
Unplugging the cable is recorded as link loss. No new network probes are sent
while disconnected: time without a link is not hidden behind a false “0% loss” or counted as sent packets.

ICMP is bound to the source IPv4 through IcmpSendEcho2Ex when WeakHostSend is disabled.
TCP is bound to the source IP and Windows IP_UNICAST_IF.
When WeakHostSend is enabled, ICMP does not start for that adapter; Windows settings are not changed.

Loss = (completed ICMP probes without a successful Echo Reply) / (all completed ICMP probes).
Pending probes and tool errors are excluded. Tool errors are shown separately.
The reply deadline is 1200 ms. Loss is shown for the last 60 seconds, 5 minutes and the entire session.
For TCP, the same metric is the fraction of failed connections, not IP packet loss.

RTT is the Echo Reply time reported by Windows; for TCP, it is connection establishment time.
p95 is the 95th percentile; a 1 ms histogram is used for the entire session.
“Jitter” here is the mean absolute RTT change between adjacent successful probes with no failed
probe between them. It measures latency variation, not game UDP jitter.

Modes
-----
Normal: one ICMP probe per second to each of the three addresses for each active adapter.
Frequent: four probes per second per address. You can switch modes in the panel without restarting.
The ICMP request payload is 32 bytes. Both modes generate little traffic and are
neither bandwidth tests nor router load tests.
“Mark failure” records the exact time for later log analysis.

How to read the result
----------------------
• Missing replies from the router and external targets together: investigate the local link/router first.
• The router responds but neither external address does: investigate WAN/provider/external routes.
• Missing replies from only one address: check that route or the node's response limits.
• Only ICMP replies are missing while TCP succeeds: ICMP filtering/rate limiting is possible; more evidence is needed.
• Ethernet Down: separate confirmation of link loss; check the cable, connector, port/adapter.
• If CoD reports loss while this test is clean, that does not disprove the game's result:
  the server, route, UDP flow, packet size and game load differ from ICMP/TCP probes.
• 0% loss does not mean no latency spikes. A reply after 254 ms counts as
  successful with a 1200 ms deadline; latency threshold exceedances are shown separately.
• Wi-Fi does not have to lose IP packets. Radio retries can deliver them successfully,
  but later. Native Wi-Fi retry counters differ from ordinary Windows adapter errors.

For comparison, collect at least 5–10 minutes on each connection under comparable conditions.
If needed, repeat separately during an ordinary file download to observe the
load relationship without starting a game. First record a result without extra load.

Logs
----
Main files are in the selected measurement folder; the default is data/statistics/.
Settings are in data/config/, service logs in data/logs/, and process state in data/runtime/.
probes.csv   — each completed probe, time, interface, source IP, target, RTT and status.
adapters.csv — state, IP, speed and error counters.
events.jsonl — disconnections/reconnections, errors, interval changes and your marks.
status.json  — latest summary and the last five minutes of history.
data/runtime/run-state.json — current dashboard address, PID and measurement path.

Panel history is bounded; CSV contains the complete saved history in the selected folder.
Files are written immediately. Collection continues until stopped; monitor free space during multi-day runs.
User passwords, traffic contents, SSID, messages and game data are not recorded.
The tool does not change network settings, drivers or router configuration.

Technical sources
-----------------
https://learn.microsoft.com/en-us/windows/win32/api/icmpapi/nf-icmpapi-icmpsendecho2ex
https://learn.microsoft.com/en-us/windows/win32/api/ipexport/ns-ipexport-icmp_echo_reply
https://learn.microsoft.com/en-us/windows/win32/winsock/ipproto-ip-socket-options
https://www.rfc-editor.org/rfc/rfc1812.html#section-4.3.2.8


The game UDP peer is identified during an active match. It may be a relay;
the next match may use a different address. Missing ICMP responses do not
allow game packet loss to be calculated. Internal and external IP/port values
on the game's network information screen may identify the user's connection
endpoint before and after NAT, rather than the game server address.

The current diagnostic runs on one PC: it compares local/external replies,
latency and Wi-Fi counters. It does not require a second computer.
In the panel's “Where the connection degrades” section, select Wi-Fi or Ethernet
and click “Check for 2 minutes”. The check is bound to the selected interface,
even when both are connected. “Stop this run” stops that run; the ordinary monitor
continues. Completed probes are compared first, with latency shown separately
from nonresponses. UDP uses standard DNS queries once per second, without
substituting TCP; this checks UDP, not CoD protocol packet loss.
A new run writes diagnosis-runs/{id}/diagnostics/{timestamp-PID}/summary.json
and probes.csv inside the selected measurement folder. Radio results are in
diagnosis-runs/{id}/radio/ with summary.json, radio.csv and raw radio-raw.jsonl counters.
Ordinary monitor cards count latency above 20 ms to the router and above 100 ms
to external targets; zero lost replies does not hide these exceedances.
The local gateway address is determined automatically for the selected adapter.
Intermediate-node replies in a separate trace may be rate-limited.
The same EXE's --udp-test / --udp-responder modes are a separate echo utility
for your own responder (see UDP-README.txt), not a step in the current diagnostic plan.


Hop-by-hop routes and official SDR relays: ROUTES-README.txt. “Route” appears beside ICMP targets.

Log folder and reset
--------------------
In “Test logs”, “Choose folder…” opens a Windows dialog.
You can also enter an absolute path and click “Apply path”. A new folder is created,
while opening a previous session's folder with probes.csv restores its history.
After opening it, “Entire test” is selected; new measurements are appended there.
The selection is saved in data/config/log-settings.json and reused after restart.
The selected folder contains the main CSV/JSONL files, routes/ snapshots, detailed
diagnosis-runs/ checks and legacy diagnostics/ and radio/ history.
Panel assets live in frontend/; target settings live in data/config/.

“Reset logs” asks for confirmation with the exact path. It clears this panel's
journals in the selected folder and accumulated metrics while retaining targets and pause state.
Unrelated files and other folders from past runs are not deleted.
Log settings are available through the gear button beside measurement status.
During a detailed check or trace, wait for completion before changing the folder
or resetting. The ordinary monitor continues running when the folder changes.
Existing user logs were not reset when this feature was introduced.

Tabs and Ethernet checks
------------------------
In “Where the connection degrades”, + adds an empty independent tab.
You can select a saved/running measurement there or start a new one with the
selected interface. Each run has its own ID, execution context, folder and stop button.
Up to four checks can run simultaneously, including on the same interface.
Stopping affects only the selected run. Closing its tab does not stop it:
a running measurement can be reopened from the list. There is no shared “current check”.
Tab and interface selections are saved in this browser. History comes from the
selected log folder; the API returns the 20 latest completed runs and all running runs.
Normal collector shutdown finishes its checks; closing the browser does not stop the collector.
“Delete record” beside the list deletes the selected completed run after you
confirm its name and folder. Stop a running record first.
Other checks continue. Only that run's owned files are deleted;
unrelated files and logs outside the selected folder are retained.

Ethernet: a detailed check polls the interface every 500 ms and counts increases
in RX/TX errors, discards, link losses/recoveries, time without a link and changes
to negotiated speed. A counter decrease starts a new baseline rather than
producing negative loss. CRC/FCS is unavailable through the standard interface
and is shown as unknown, not 0. Zero counters on the PC do not prove error-free
PC→router transmission: damage may be counted only at the receiving router port.
Link events are correlated with local/external ICMP probes within a ±1 second window.
Timing coincidence does not establish cause; polling can miss an outage shorter than 500 ms.

Each new detailed run has diagnosis-runs/{id}/run.json and separate
diagnostics-live.json, radio-live.json or ethernet-live.json results, plus raw CSV.
Runs in the selected folder remain available after restart; reset clears their
owned files and history records while retaining unrelated files.
Dashboard has separate Wi-Fi/Ethernet tabs: one dashboard is shown at a time.
The browser saves the selection; it does not affect the other interface's measurements.
Below the top connection cards is the “Tests / Dashboard” switch.
“Tests” contains independent measurements and game server setup; “Dashboard” contains
ordinary Wi-Fi/Ethernet dashboards, monitor controls and events. Switching sections
does not stop background measurements; the browser remembers the selected section.
Ethernet counter definition sources: ethernet-methodology.txt.
“Route” expands details inside its card and widens the card to the full row.
The card contains launch controls, hops, state and trace conclusions.
Several cards can be expanded at once. Collapsing hides details without cancelling
the check; refreshing the panel retains expanded state.

Explanations marked in the interface open through white circled ? icons.
Tooltips are available on hover, keyboard focus or click. Escape closes the tooltip
first. Important results, progress and errors remain visible.
Tooltips appear above their icon; long text scrolls within the available space
above. Position is adjusted near the top edge of the window.
The automatic Relay detection explanation also opens through ? beside its button.
The + button is immediately after the measurement tabs.

“Game server” provides CS2 / Call of Duty · Steam selection. CS2 retains the Valve
catalog; CoD shows regional control targets in Hong Kong and Singapore.
“Automatically add active Relay” starts a twenty-second observation of cod.exe
UDP through Windows ETW with a UAC request. A confirmed peer is added to the ordinary
cards, and its UDP port is stored separately from TCP probes.
Methodology, limitations, logs and API are described in COD-RELAY-README.txt.

Source and data structure
-------------------------
frontend/ contains the English panel and public catalogs;
the native window and tray live in frontend/desktop/. HTTP API lives in backend/api/,
the ordinary monitor in backend/networkmanager/, independent checks in
backend/networkmanager/diagnostics/, and Windows collectors in backend/networkmanager/collectors/.
API and NetworkManager are packages in one backend process.
backend/app/ manages its lifecycle, backend/config/ manages settings, and
backend/infrastructure/ provides shared technical functions. The native folder
picker is injected from frontend through a callback; backend does not import desktop UI.
All tests live separately in tests/. Build from the root: go run ./cmd/build.
Settings are imported from legacy InternetStability-data, internet-stability and
%APPDATA%/InternetStability; import neither moves nor clears history.
An explicitly selected measurement folder is retained instead of data/statistics/.
