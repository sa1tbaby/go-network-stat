ROUTES AND OFFICIAL REGIONAL ADDRESSES
=====================================

The panel has a “Route” button beside each ICMP target.
It expands information inside that card and widens it to the full row.
“Check route” starts a separate TTL probe series; clicking it again collects
a new snapshot. Collapsing the section does not cancel a running series.

Parameters: IPv4, the same source IP as the selected Wi-Fi/Ethernet; up to 25 TTLs;
20 probes per hop (initial discovery plus repeat rounds), 700 ms timeout,
up to 8 simultaneous probes and a total budget of about 90 seconds.
Route checks are independent of the ordinary test's pause state and start only with an explicit click.
At most 2 routes may run simultaneously; an IP change or adapter disconnection interrupts the series.

Displayed information:
• TTL (hop number in the diagnostic route).
• Responding node IPs; multiple addresses for one TTL can reflect balancing/path changes.
• Responses/completed probes, with tool errors shown separately.
• Fraction of probes that received NO ICMP response.
• Mean/minimum/maximum RTT.
• Cautious assessment: responding, final target, ICMP limited, no response,
  node reported Unreachable, or a possible problematic segment.

“No responses” does NOT represent the percentage of transit game traffic lost by that router.
If an intermediate node does not respond but the final target responds 20/20,
forwarding to the target worked in this series. Routers can rate-limit TTL-expired replies.
Even consistent missing responses from one hop through the destination indicate only a suspect segment:
ICMP can follow a different return path, be rate-limited or be handled differently.
RTT is the round-trip time to a node, not the latency of one link or the difference between adjacent rows.
Game UDP can follow a different path from ICMP; tracing alone does not identify
the exact cause of game loss. Repeat during symptoms and compare with UDP telemetry.

The official Valve configuration for appid730 publishes Steam SDR relays:
Singapore (Southeast Asia): 103.10.124.116, .117, .118, .119, .120, .121
All specify UDP27015–27060. This is not a TCP port or a guaranteed CS2 match-server IP list.
Hong Kong in the panel is a neighboring region, not Singapore or Southeast Asia itself.
Sources and snapshot time are recorded in official-relays.json.
The catalog is a snapshot of published configuration; addresses and POP availability can change.
POP sea means Seattle, not Southeast Asia.

For CoD MW4, no officially published fixed list of Southeast Asian match-server
IPs was found. Activision port tables are not IP lists; old entries for other games
must not be presented as MW4 information. CoD being available on Steam does not prove
that it uses Valve SDR: game addresses must be identified from the actual match connection.
No previously found CoD TCP peer was added as a confirmed game UDP server.

For example, if an intermediate TTL does not respond but the final node answers all probes,
that does not prove transit traffic loss at the intermediate node.
Route snapshots are saved in routes/*.json in the selected log folder.

Primary sources:
https://api.steampowered.com/ISteamApps/GetSDRConfig/v1/?appid=730
https://partner.steamgames.com/doc/features/multiplayer/steamdatagramrelay?l=english
https://support.activision.com/articles/ports-used-for-call-of-duty-games
https://learn.microsoft.com/en-us/windows-server/administration/windows-commands/tracert
https://learn.microsoft.com/en-us/windows/win32/api/icmpapi/nf-icmpapi-icmpsendecho2ex
https://www.rfc-editor.org/rfc/rfc1812.html#section-4.3.2.8

Route implementation lives in backend/networkmanager/routes/; panel requests go through backend/api/.
Data sources are frontend/official-relays.json and frontend/cod-test-targets.json.
The routes/*.json snapshots belong to the selected measurement store (by default
data/statistics/), not the source directory. All tests live in tests/.
