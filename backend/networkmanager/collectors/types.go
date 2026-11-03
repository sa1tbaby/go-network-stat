// Package collectors provides source-bound network probes and native interface
// telemetry. Protocols and counter semantics are shared across platforms; the
// Windows implementation talks directly to IP Helper and Native Wi-Fi APIs.
package collectors

import (
	"context"
	"errors"
	"time"
)

var ErrWindowsRequired = errors.New("native network telemetry requires Windows")

type Adapter struct {
	ID                string  `json:"id"`
	Name              string  `json:"name"`
	Kind              string  `json:"kind"`
	State             string  `json:"state"`
	IPv4              string  `json:"ipv4"`
	Gateway           string  `json:"gateway"`
	Index             int     `json:"index"`
	SpeedMbps         float64 `json:"speedMbps"`
	RxErrors          int64   `json:"rxErrors"`
	TxErrors          int64   `json:"txErrors"`
	RxDiscards        int64   `json:"rxDiscards"`
	TxDiscards        int64   `json:"txDiscards"`
	LastChange        string  `json:"lastChange"`
	ICMPSafe          bool    `json:"icmpSafe"`
	Available         bool    `json:"available"`
	InterfaceGUID     string  `json:"interfaceGuid,omitempty"`
	CountersAvailable bool    `json:"-"`
	Error             string  `json:"-"`
}

type ProbeRequest struct {
	Source         string
	Address        string
	InterfaceIndex int
	TTL            int
	PayloadBytes   int
	TimeoutMs      int
	// DNSPort selects the DNS server's UDP port for DNS and StreamDNS.
	// Zero uses the standard DNS port 53.
	DNSPort int
	// DurationMs limits sending in StreamDNS; its final outstanding requests
	// are drained to their deadlines. Zero sends until owner cancellation.
	DurationMs int
	// OnPending reports StreamDNS in-flight requests on its scheduler goroutine.
	// It is private telemetry, never a serialized request/API field.
	OnPending func(int) `json:"-"`
}

type ProbeResult struct {
	OK           bool     `json:"ok"`
	RTTMs        *float64 `json:"rttMs"`
	Status       string   `json:"status"`
	ToolError    bool     `json:"toolError"`
	ReplyAddress string   `json:"replyAddress,omitempty"`
	Bytes        int      `json:"bytes"`
	// Received also includes TTL-exceeded and unreachable ICMP replies. Such
	// replies are useful to traceroute but are not successful echo probes.
	Received bool   `json:"received,omitempty"`
	IPStatus uint32 `json:"ipStatus,omitempty"`
}

type Collector interface {
	Adapters(context.Context) ([]Adapter, error)
	ICMP(context.Context, ProbeRequest) ProbeResult
	TCP(context.Context, ProbeRequest, int) ProbeResult
	DNS(context.Context, ProbeRequest) ProbeResult
	Radio(context.Context, Adapter) (RadioSample, error)
}

type DNSObservation struct {
	Sent   time.Time
	At     time.Time
	Lag    float64
	Result ProbeResult
	Late   bool
}

// DNSStreamCollector retains a source-bound UDP socket and expired IDs so a
// reply after its deadline can be distinguished from a fresh successful probe.
type DNSStreamCollector interface {
	StreamDNS(context.Context, ProbeRequest, int, func(DNSObservation)) error
}

// PHYStats contains driver counters for one PHY. Never sum identical PHYs:
// several Windows drivers expose the same hardware counters in every slot.
type PHYStats struct {
	Index                  int    `json:"index"`
	TxFrames               uint64 `json:"txFrames"`
	MulticastTxFrames      uint64 `json:"multicastTxFrames"`
	FailedFrames           uint64 `json:"failedFrames"`
	RetryFrames            uint64 `json:"retryFrames"`
	MultipleRetryFrames    uint64 `json:"multipleRetryFrames"`
	TxLifetimeExceeded     uint64 `json:"txLifetimeExceeded"`
	TxFragments            uint64 `json:"txFragments"`
	RTSSuccess             uint64 `json:"rtsSuccess"`
	RTSFailure             uint64 `json:"rtsFailure"`
	ACKFailure             uint64 `json:"ackFailure"`
	RxFrames               uint64 `json:"rxFrames"`
	MulticastRxFrames      uint64 `json:"multicastRxFrames"`
	PromiscuousRxFrames    uint64 `json:"promiscuousRxFrames"`
	RxLifetimeExceeded     uint64 `json:"rxLifetimeExceeded"`
	DuplicateFrames        uint64 `json:"duplicateFrames"`
	RxFragments            uint64 `json:"rxFragments"`
	PromiscuousRxFragments uint64 `json:"promiscuousRxFragments"`
	FCSErrors              uint64 `json:"fcsErrors"`
}

type RadioSample struct {
	Time                 string         `json:"time"`
	InterfaceGUID        string         `json:"interfaceGuid"`
	StatisticsError      uint32         `json:"statisticsError"`
	StatisticsAvailable  bool           `json:"statisticsAvailable"`
	RSSIDbm              *int32         `json:"rssiDbm"`
	QualityPct           *uint32        `json:"qualityPct"`
	TxRateNative         *uint32        `json:"txRateNative"`
	RxRateNative         *uint32        `json:"rxRateNative"`
	Phys                 []PHYStats     `json:"phys"`
	DuplicatePHYCounters bool           `json:"duplicatePhyCounters"`
	Raw                  map[string]any `json:"raw,omitempty"`
	Error                string         `json:"error,omitempty"`
}
