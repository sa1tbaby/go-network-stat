package routes

import (
	"fmt"
	"math"
	"strings"

	"github.com/sa1tbaby/go-network-stat/backend/networkmanager/collectors"
)

func (engine *Engine) addHopReply(job *Job, hop *Hop, reply collectors.ProbeResult) {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	hop.Sent++
	hop.ProbeStatuses = append(hop.ProbeStatuses, reply.Status)
	if reply.ToolError {
		hop.ToolErrors++
	} else if reply.Received || reply.OK {
		hop.Received++
		if reply.ReplyAddress != "" {
			hop.replyAddresses = append(hop.replyAddresses, reply.ReplyAddress)
		}

		if reply.RTTMs != nil {
			hop.rtts = append(hop.rtts, *reply.RTTMs)
		}
	}

	if reply.OK && (reply.ReplyAddress == "" || reply.ReplyAddress == job.Address) {
		hop.echoReplies++
		job.DestinationReached = true
	}

	if reply.Received && (reply.IPStatus == 11002 || reply.IPStatus == 11003 || reply.IPStatus == 11004 || reply.IPStatus == 11005 || reply.IPStatus == 11012 || strings.Contains(reply.Status, "Unreachable")) {
		hop.unreachableReplies++
	}

	usable := hop.Sent - hop.ToolErrors
	if usable > 0 {
		hop.NoReplyPct = floatPtr(100 * float64(usable-hop.Received) / float64(usable))
	}

	counts := map[string]int{}
	hop.Addresses = []string{}
	highest := 0
	for _, address := range hop.replyAddresses {
		if counts[address] == 0 {
			hop.Addresses = append(hop.Addresses, address)
		}

		counts[address]++
		if counts[address] > highest {
			highest = counts[address]
			hop.Address = stringPtr(address)
		}
	}

	if len(hop.rtts) > 0 {
		sum, min, max := 0.0, hop.rtts[0], hop.rtts[0]
		for _, rtt := range hop.rtts {
			sum += rtt
			min = math.Min(min, rtt)
			max = math.Max(max, rtt)
		}

		hop.AvgMs = floatPtr(sum / float64(len(hop.rtts)))
		hop.MinMs = &min
		hop.MaxMs = &max
	}

	job.UpdatedAt = iso()
}

func (engine *Engine) assessRouteLocked(job *Job) {
	var destination *Hop
	for _, h := range job.Hops {
		if h.echoReplies > 0 {
			destination = h
		}
	}

	for _, h := range job.Hops {
		later := false
		for _, next := range job.Hops {
			if next.TTL > h.TTL && next.Received > 0 {
				later = true
			}
		}

		note := ""
		switch {
		case h.echoReplies > 0:
			h.Assessment = "destination"
			note = fmt.Sprintf("The destination returned Echo Reply: %d/%d. These are ICMP replies and do not measure game UDP.", h.echoReplies, h.Sent-h.ToolErrors)
		case h.unreachableReplies > 0:
			h.Assessment = "unreachable"
			note = "This hop reported ICMP Unreachable. The message does not prove loss on a specific physical link."
		case h.Received == 0:
			if later {
				h.Assessment = "response_limit_possible"
				note = "This hop does not reply, but later hops are reachable; ICMP filtering or rate limiting is possible. Transit loss is not proven here."
			} else {
				h.Assessment = "no_reply"
				note = "No diagnostic replies. The cause is unknown; this hop may not respond to ICMP."
			}
		case h.NoReplyPct != nil && *h.NoReplyPct > 0 && later && (destination == nil || destination.NoReplyPct != nil && *destination.NoReplyPct == 0):
			h.Assessment = "response_limit_possible"
			note = "Some replies are missing, but later hops respond. Diagnostic replies may be rate limited."
		default:
			h.Assessment = "responding"
			note = "RTT is the round-trip time to this hop, not the latency of an individual link."
		}

		h.Note = &note
	}

	if destination == nil {
		job.Conclusion = "The destination did not return Echo Reply. The route is only partially visible; ICMP filtering cannot be distinguished from an unreachable destination, and the affected link cannot be identified precisely."
	} else if destination.echoReplies == destination.Sent-destination.ToolErrors {
		job.Conclusion = fmt.Sprintf("The destination replied to all %d completed diagnostic probes. Missing intermediate replies do not prove transit loss. This test does not check game UDP.", destination.Sent-destination.ToolErrors)
	} else if destination.echoReplies < destination.Received {
		job.Conclusion = fmt.Sprintf("At the destination TTL, %d Echo Replies were received from %d probes; some probes received other ICMP replies. The route length or path may have changed. This mixed series cannot reliably localize loss.", destination.echoReplies, destination.Sent-destination.ToolErrors)
	} else {
		var candidate *Hop
		for _, h := range job.Hops {
			if h.TTL >= destination.TTL || h.Sent-h.ToolErrors < 10 || h.NoReplyPct == nil || *h.NoReplyPct < 10 {
				continue
			}

			consistent := true
			for _, next := range job.Hops {
				if next.Received > 0 && next.TTL >= h.TTL && next.TTL <= destination.TTL && (next.NoReplyPct == nil || *next.NoReplyPct < 10) {
					consistent = false
				}
			}

			if consistent {
				candidate = h
				break
			}
		}

		if candidate != nil {
			candidate.Assessment = "possible_path_issue"
			candidate.Note = stringPtr("From these probes onward, missing replies also appear at later hops through the destination. This suggests a possible affected segment without proving the cause; the return path and ICMP rate limits may contribute.")
			job.Conclusion = fmt.Sprintf("The destination has missing replies; the first consistent symptom appears around TTL %d. The exact location is not proven. Compare with game UDP and repeat the test.", candidate.TTL)
		} else {
			loss := 0.0
			if destination.NoReplyPct != nil {
				loss = *destination.NoReplyPct
			}

			job.Conclusion = fmt.Sprintf("The destination is missing %.1f%% of diagnostic replies. This series cannot reliably localize the affected segment; the forward path, return path, or destination ICMP rate limits may contribute.", loss)
		}
	}

	for _, h := range job.Hops {
		if h.ToolErrors > 0 {
			job.Conclusion += " Tool errors are excluded from the percentage and reported separately."
			break
		}
	}
}
