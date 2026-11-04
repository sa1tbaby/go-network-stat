package collectors

type UDPEvent struct {
	UnixMs     int64  `json:"unixMs"`
	PID        int    `json:"pid"`
	Outgoing   bool   `json:"outgoing"`
	LocalIP    string `json:"localIp"`
	LocalPort  int    `json:"localPort"`
	RemoteIP   string `json:"remoteIp"`
	RemotePort int    `json:"remotePort"`
	Size       int    `json:"size"`
}

type UDPPeer struct {
	RemoteIP             string  `json:"remoteIp"`
	RemotePort           int     `json:"remotePort"`
	LocalIP              string  `json:"localIp"`
	LocalPort            int     `json:"localPort"`
	OutEvents            int     `json:"outEvents"`
	InEvents             int     `json:"inEvents"`
	OutBytes             int64   `json:"outBytes"`
	InBytes              int64   `json:"inBytes"`
	FirstUnixMs          int64   `json:"firstUnixMs"`
	LastUnixMs           int64   `json:"lastUnixMs"`
	DurationSeconds      float64 `json:"durationSeconds"`
	BidirectionalWindows int     `json:"bidirectionalWindows"`
	PacketShare          float64 `json:"packetShare"`
	Eligible             bool    `json:"eligible"`
}

type UDPAnalysis struct {
	State         string    `json:"state"`
	Message       string    `json:"message"`
	GamePID       int       `json:"gamePid"`
	GameUDPEvents int       `json:"gameUdpEvents"`
	Selected      *UDPPeer  `json:"selected"`
	Candidates    []UDPPeer `json:"candidates"`
}
