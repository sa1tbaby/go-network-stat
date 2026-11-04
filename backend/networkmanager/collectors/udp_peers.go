package collectors

import (
	"sort"
	"strconv"
)

func groupGameUDPEvents(input []UDPEvent, pid int, start, end int64) (map[string][]UDPEvent, int, int) {
	excluded := map[int]bool{
		53:   true,
		67:   true,
		68:   true,
		80:   true,
		123:  true,
		443:  true,
		1900: true,
		5353: true,
		5355: true,
	}
	groups := map[string][]UDPEvent{}
	public := 0
	observed := 0
	for _, event := range input {
		if event.PID != pid || event.UnixMs < start || event.UnixMs > end {
			continue
		}

		observed++
		if !PublicIPv4(event.RemoteIP) || excluded[event.RemotePort] {
			continue
		}

		public++
		if event.RemotePort < 1 || event.RemotePort > 65535 {
			continue
		}

		key := event.LocalIP + "|" + strconv.Itoa(event.LocalPort) + "|" + event.RemoteIP + "|" + strconv.Itoa(event.RemotePort)
		groups[key] = append(groups[key], event)
	}

	return groups, public, observed
}

func summarizeUDPPeer(rows []UDPEvent, start, end int64, public int) UDPPeer {
	sort.Slice(rows, func(i, j int) bool {
		return rows[i].UnixMs < rows[j].UnixMs
	})
	first := rows[0]
	last := rows[len(rows)-1]
	peer := UDPPeer{
		LocalIP:         first.LocalIP,
		LocalPort:       first.LocalPort,
		RemoteIP:        first.RemoteIP,
		RemotePort:      first.RemotePort,
		FirstUnixMs:     first.UnixMs,
		LastUnixMs:      last.UnixMs,
		DurationSeconds: float64(last.UnixMs-first.UnixMs) / 1000,
	}
	windows := map[int64]uint8{}
	for _, event := range rows {
		bucket := (event.UnixMs - start) / 5000
		if event.Outgoing {
			peer.OutEvents++
			peer.OutBytes += int64(event.Size)
			windows[bucket] |= 1
		} else {
			peer.InEvents++
			peer.InBytes += int64(event.Size)
			windows[bucket] |= 2
		}
	}

	for _, flags := range windows {
		if flags == 3 {
			peer.BidirectionalWindows++
		}
	}

	if public > 0 {
		peer.PacketShare = float64(len(rows)) / float64(public)
	}

	peer.Eligible = peer.OutEvents >= 80 && peer.InEvents >= 80 && len(rows) >= 240 && peer.DurationSeconds >= 10 && peer.BidirectionalWindows >= 3 && end-peer.LastUnixMs <= 3000

	return peer
}
