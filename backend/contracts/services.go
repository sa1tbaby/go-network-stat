package contracts

import "github.com/sa1tbaby/go-network-stat/backend/networkmanager/collectors"

// ServiceHost keeps diagnostic work independent of the monitor's state lock.
// Callbacks must never be called while a service mutex is held.
type ServiceHost interface {
	Adapter(string) (collectors.Adapter, bool)
	LogDirectory() string
	StateDirectory() string
	Event(string, string)
	AddDetectedTarget(string, DetectedTarget, string) error
}

type DetectedTarget struct {
	Address    string
	SourceIP   string
	UDPPort    int
	LocalPort  int
	InEvents   int
	OutEvents  int
	ObservedAt string
	Label      string
}

type DiagnosisService interface {
	Status() any
	Start(string) (any, error)
	Stop(string) (any, error)
	Delete(string, string) (any, error)
	Active() bool
	Close()
}

type RelayService interface {
	Status(string) any
	Start(string) (any, error)
	Active() bool
	Close()
}
