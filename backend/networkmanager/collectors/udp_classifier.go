package collectors

import "sort"

// ClassifyUDP identifies a sustained bidirectional peer. ETW event counts cannot
// establish game packet loss: neither sequence numbers nor a remote receiver
// are available here.
func ClassifyUDP(input []UDPEvent, pid int, start, end int64, game string) UDPAnalysis {
	name := "CoD"
	process := "cod.exe"
	if game == "cs2" {
		name = "CS2"
		process = "cs2.exe"
	}

	result := UDPAnalysis{
		State:      "no_active_flow",
		Message:    "No sustained game UDP flow was found. Start a match in " + name + " and try again; brief authentication and QoS traffic is not added.",
		GamePID:    pid,
		Candidates: []UDPPeer{},
	}
	groups, public, observed := groupGameUDPEvents(input, pid, start, end)
	result.GameUDPEvents = observed
	peers := make([]UDPPeer, 0, len(groups))
	for _, rows := range groups {
		peer := summarizeUDPPeer(rows, start, end, public)
		peers = append(peers, peer)
	}

	sort.Slice(peers, func(i, j int) bool {
		a, b := peers[i], peers[j]
		if a.OutEvents+a.InEvents == b.OutEvents+b.InEvents {
			return a.RemoteIP < b.RemoteIP
		}

		return a.OutEvents+a.InEvents > b.OutEvents+b.InEvents
	})
	for i := range peers {
		if i < 8 {
			result.Candidates = append(result.Candidates, peers[i])
		}
	}

	best := -1
	for i, peer := range peers {
		if peer.Eligible {
			best = i
			break
		}
	}

	if best < 0 {
		return result
	}

	peer := peers[best]
	ambiguous := peer.PacketShare < .60
	for i, other := range peers {
		if i != best && float64(other.OutEvents+other.InEvents) >= .5*float64(peer.OutEvents+peer.InEvents) {
			ambiguous = true
			break
		}
	}

	if ambiguous {
		result.State = "ambiguous"
		result.Message = "Several comparable UDP flows were found. No address was added automatically because a voice or service flow could be mistaken for game traffic."

		return result
	}

	result.State = "detected"
	result.Selected = &peer
	result.Message = "A sustained bidirectional UDP flow was confirmed for " + process + ". The peer may be a relay or server; ETW does not measure the percentage of game UDP loss."

	return result
}
