package collectors

type CaptureRequest struct {
	AppID           string `json:"appId"`
	Game            string `json:"game"`
	RunID           string `json:"runId"`
	GamePID         int    `json:"gamePid"`
	GameStartTicks  int64  `json:"gameStartTicks"`
	OwnerPID        int    `json:"ownerPid"`
	OwnerStartTicks int64  `json:"ownerStartTicks"`
	Started         string `json:"start"`
}

type CaptureReport struct {
	AppID             string       `json:"appId"`
	Game              string       `json:"game"`
	RunID             string       `json:"runId"`
	GamePID           int          `json:"gamePid"`
	GameStartTicks    int64        `json:"gameStartTicks"`
	State             string       `json:"state"`
	Message           string       `json:"message"`
	Started           string       `json:"started"`
	Updated           string       `json:"updated"`
	Completed         string       `json:"completed"`
	DurationSeconds   int          `json:"durationSeconds"`
	Session           string       `json:"session"`
	ETL               string       `json:"etl"`
	Error             *string      `json:"error"`
	Analysis          *UDPAnalysis `json:"analysis"`
	CaptureStartMs    int64        `json:"captureStartMs"`
	CaptureEndMs      int64        `json:"captureEndMs"`
	TotalETWEvents    int          `json:"totalEtwEvents"`
	IPv6GameUDPEvents int          `json:"ipv6GameUdpEvents"`
	PayloadPIDUsed    bool         `json:"payloadPidUsed"`
}
