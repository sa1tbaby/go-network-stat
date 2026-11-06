package routes

type Hop struct {
	TTL                int      `json:"ttl"`
	Address            *string  `json:"address"`
	Addresses          []string `json:"addresses"`
	Sent               int      `json:"sent"`
	Received           int      `json:"received"`
	ToolErrors         int      `json:"toolErrors"`
	NoReplyPct         *float64 `json:"noReplyPct"`
	AvgMs              *float64 `json:"avgMs"`
	MinMs              *float64 `json:"minMs"`
	MaxMs              *float64 `json:"maxMs"`
	ProbeStatuses      []string `json:"probeStatuses"`
	Assessment         string   `json:"assessment"`
	Note               *string  `json:"note"`
	rtts               []float64
	replyAddresses     []string
	echoReplies        int
	unreachableReplies int
}

type Job struct {
	AppID              string  `json:"appId"`
	State              string  `json:"state"`
	AdapterID          string  `json:"adapterId"`
	Address            string  `json:"address"`
	StartedAt          string  `json:"startedAt"`
	UpdatedAt          string  `json:"updatedAt"`
	Error              *string `json:"error"`
	Hops               []*Hop  `json:"hops"`
	DestinationReached bool    `json:"destinationReached"`
	Conclusion         string  `json:"conclusion"`
	SampleGoal         int     `json:"sampleGoal"`
	TimeoutMs          int     `json:"timeoutMs"`
	MaxHops            int     `json:"maxHops"`
	Phase              string  `json:"phase"`
	LogFile            string  `json:"logFile"`
	source             string
	index              int
}
