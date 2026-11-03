//go:build windows

package collectors

import (
	"context"
	"fmt"
	"net"
	"runtime"
	"sort"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

var ipHelper = syscall.NewLazyDLL("iphlpapi.dll")

var getIfTable2 = ipHelper.NewProc("GetIfTable2")

var freeMibTable = ipHelper.NewProc("FreeMibTable")

var getAdaptersAddresses = ipHelper.NewProc("GetAdaptersAddresses")

var getIPInterfaceEntry = ipHelper.NewProc("GetIpInterfaceEntry")

type nativeCollector struct {
	mu       sync.Mutex
	previous map[string]Adapter
}

func New() Collector {
	return &nativeCollector{previous: make(map[string]Adapter)}
}

type nativeGUID struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

func (g nativeGUID) String() string {
	return fmt.Sprintf("%08x-%04x-%04x-%02x%02x-%02x%02x%02x%02x%02x%02x", g.Data1, g.Data2, g.Data3, g.Data4[0], g.Data4[1], g.Data4[2], g.Data4[3], g.Data4[4], g.Data4[5], g.Data4[6], g.Data4[7])
}

// Native SDK structures: keep explicit byte BOOLEANs and padding rather than
// using Go bool. Definitions follow netioapi.h; AMD64 offsets are tested below.
type mibIfRow2 struct {
	Luid                     uint64
	Index                    uint32
	GUID                     nativeGUID
	Alias                    [257]uint16
	Description              [257]uint16
	PhysicalAddressLength    uint32
	PhysicalAddress          [32]byte
	PermanentPhysicalAddress [32]byte
	MTU                      uint32
	Type                     uint32
	TunnelType               uint32
	MediaType                uint32
	PhysicalMediumType       uint32
	AccessType               uint32
	DirectionType            uint32
	Flags                    uint8
	_                        [3]byte
	OperStatus               uint32
	AdminStatus              uint32
	MediaConnectState        uint32
	NetworkGUID              nativeGUID
	ConnectionType           uint32
	TransmitLinkSpeed        uint64
	ReceiveLinkSpeed         uint64
	InOctets                 uint64
	InUcastPkts              uint64
	InNUcastPkts             uint64
	InDiscards               uint64
	InErrors                 uint64
	InUnknownProtos          uint64
	InUcastOctets            uint64
	InMulticastOctets        uint64
	InBroadcastOctets        uint64
	OutOctets                uint64
	OutUcastPkts             uint64
	OutNUcastPkts            uint64
	OutDiscards              uint64
	OutErrors                uint64
	OutUcastOctets           uint64
	OutMulticastOctets       uint64
	OutBroadcastOctets       uint64
	OutQLen                  uint64
}

type mibIPInterfaceRow struct {
	Family                               uint16
	_                                    [6]byte
	Luid                                 uint64
	Index                                uint32
	MaxReassemblySize                    uint32
	InterfaceIdentifier                  uint64
	MinRouterAdvertisementInterval       uint32
	MaxRouterAdvertisementInterval       uint32
	AdvertisingEnabled                   uint8
	ForwardingEnabled                    uint8
	WeakHostSend                         uint8
	WeakHostReceive                      uint8
	UseAutomaticMetric                   uint8
	UseNeighborUnreachabilityDetection   uint8
	ManagedAddressConfigurationSupported uint8
	OtherStatefulConfigurationSupported  uint8
	AdvertiseDefaultRoute                uint8
	_                                    [3]byte
	RouterDiscoveryBehavior              uint32
	DadTransmits                         uint32
	BaseReachableTime                    uint32
	RetransmitTime                       uint32
	PathMTUDiscoveryTimeout              uint32
	LinkLocalAddressBehavior             uint32
	LinkLocalAddressTimeout              uint32
	ZoneIndices                          [16]uint32
	SitePrefixLength                     uint32
	Metric                               uint32
	NLMTU                                uint32
	Connected                            uint8
	SupportsWakeUpPatterns               uint8
	SupportsNeighborDiscovery            uint8
	SupportsRouterDiscovery              uint8
	ReachableTime                        uint32
	TransmitOffload                      uint8
	ReceiveOffload                       uint8
	DisableDefaultRoutes                 uint8
	_                                    [1]byte
}

type nativeSocketAddress struct {
	Address *syscall.RawSockaddrAny
	Length  int32
}

type adapterAddressNode struct {
	Length  uint32
	Flags   uint32
	Next    *adapterAddressNode
	Address nativeSocketAddress
}

type ipAdapterAddresses struct {
	Length                uint32
	Index                 uint32
	Next                  *ipAdapterAddresses
	AdapterName           *byte
	FirstUnicast          *adapterAddressNode
	FirstAnycast          *adapterAddressNode
	FirstMulticast        *adapterAddressNode
	FirstDNS              *adapterAddressNode
	DNSSuffix             *uint16
	Description           *uint16
	FriendlyName          *uint16
	PhysicalAddress       [8]byte
	PhysicalAddressLength uint32
	Flags                 uint32
	MTU                   uint32
	IfType                uint32
	OperStatus            uint32
	IPv6IfIndex           uint32
	ZoneIndices           [16]uint32
	FirstPrefix           uintptr
	TransmitLinkSpeed     uint64
	ReceiveLinkSpeed      uint64
	FirstWins             *adapterAddressNode
	FirstGateway          *adapterAddressNode
}

type ipv4Binding struct {
	source  string
	gateway string
}

func ipv4FromSocket(address nativeSocketAddress) string {
	if address.Address == nil || address.Length < 16 || address.Address.Addr.Family != syscall.AF_INET {
		return ""
	}

	v4 := (*syscall.RawSockaddrInet4)(unsafe.Pointer(address.Address))

	return net.IP(v4.Addr[:]).String()
}

func readIPv4Bindings() (map[uint32]ipv4Binding, error) {
	size := uint32(15000)
	for attempt := 0; attempt < 5; attempt++ {
		if size > 16<<20 {
			return nil, fmt.Errorf("GetAdaptersAddresses returned excessive size: %d", size)
		}

		buffer := make([]byte, size)
		result, _, _ := getAdaptersAddresses.Call(syscall.AF_INET, 0x80|0x100|2|4|8, 0, uintptr(unsafe.Pointer(&buffer[0])), uintptr(unsafe.Pointer(&size)))
		if result == 111 {
			continue
		}

		if result == 232 {
			return map[uint32]ipv4Binding{}, nil
		}

		if result != 0 {
			return nil, fmt.Errorf("GetAdaptersAddresses: %w", syscall.Errno(result))
		}

		bindings := make(map[uint32]ipv4Binding)
		for a, n := (*ipAdapterAddresses)(unsafe.Pointer(&buffer[0])), 0; a != nil && n < 65536; a, n = a.Next, n+1 {
			if a.Length < uint32(unsafe.Offsetof(a.FirstGateway)+unsafe.Sizeof(a.FirstGateway)) {
				continue
			}

			binding := ipv4Binding{}
			for u, count := a.FirstUnicast, 0; u != nil && count < 1024; u, count = u.Next, count+1 {
				ip := net.ParseIP(ipv4FromSocket(u.Address))
				if ip != nil && !ip.IsUnspecified() && !ip.IsLinkLocalUnicast() {
					binding.source = ip.String()
					break
				}
			}

			for g, count := a.FirstGateway, 0; g != nil && count < 1024; g, count = g.Next, count+1 {
				ip := net.ParseIP(ipv4FromSocket(g.Address))
				if ip != nil && !ip.IsUnspecified() {
					binding.gateway = ip.String()
					break
				}
			}

			bindings[a.Index] = binding
		}

		runtime.KeepAlive(buffer)

		return bindings, nil
	}

	return nil, fmt.Errorf("network configuration changed while reading adapters")
}

func strongHost(index uint32) (bool, error) {
	row := mibIPInterfaceRow{Family: syscall.AF_INET, Index: index}
	result, _, _ := getIPInterfaceEntry.Call(uintptr(unsafe.Pointer(&row)))
	if result != 0 {
		return false, fmt.Errorf("GetIpInterfaceEntry: %w", syscall.Errno(result))
	}

	return row.WeakHostSend == 0, nil
}

func operationalState(value uint32) string {
	switch value {
	case 1:
		return "Up"
	case 2:
		return "Down"
	case 3:
		return "Testing"
	case 5:
		return "Dormant"
	case 6:
		return "NotPresent"
	case 7:
		return "LowerLayerDown"
	default:
		return "Unknown"
	}
}

func (c *nativeCollector) Adapters(ctx context.Context) ([]Adapter, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	bindings, err := readIPv4Bindings()
	if err != nil {
		return nil, err
	}

	var table unsafe.Pointer
	result, _, _ := getIfTable2.Call(uintptr(unsafe.Pointer(&table)))
	if result != 0 {
		return nil, fmt.Errorf("GetIfTable2: %w", syscall.Errno(result))
	}

	if table == nil {
		return nil, fmt.Errorf("GetIfTable2 returned no table")
	}

	defer freeMibTable.Call(uintptr(table))
	count := *(*uint32)(table)
	if count > 65536 {
		return nil, fmt.Errorf("too many native interfaces: %d", count)
	}

	type tableLayout struct {
		Count uint32
		Rows  [1]mibIfRow2
	}
	rows := unsafe.Slice((*mibIfRow2)(unsafe.Add(table, unsafe.Offsetof(tableLayout{}.Rows))), int(count))
	adapters := make([]Adapter, 0, len(rows))
	now := time.Now().Format(time.RFC3339Nano)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.previous == nil {
		c.previous = make(map[string]Adapter)
	}

	for _, row := range rows {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		// Match Get-NetAdapter -Physical, excluding virtual switches/tunnels.
		if row.Flags&1 == 0 || row.Flags&128 != 0 || (row.Type != 6 && row.Type != 71) {
			continue
		}

		name := syscall.UTF16ToString(row.Alias[:])
		if name == "" {
			name = syscall.UTF16ToString(row.Description[:])
		}

		binding := bindings[row.Index]
		a := Adapter{
			ID:                name,
			Name:              name,
			Kind:              "ethernet",
			State:             operationalState(row.OperStatus),
			IPv4:              binding.source,
			Gateway:           binding.gateway,
			Index:             int(row.Index),
			SpeedMbps:         float64(row.TransmitLinkSpeed) / 1e6,
			RxErrors:          int64(row.InErrors),
			TxErrors:          int64(row.OutErrors),
			RxDiscards:        int64(row.InDiscards),
			TxDiscards:        int64(row.OutDiscards),
			InterfaceGUID:     row.GUID.String(),
			CountersAvailable: true,
			LastChange:        now,
		}
		if row.Type == 71 {
			a.Kind = "wifi"
		}

		a.Available = a.State == "Up" && a.IPv4 != "" && a.Index > 0
		if a.Available {
			var e error
			a.ICMPSafe, e = strongHost(row.Index)
			if e != nil {
				a.Error = e.Error()
			}
		}

		if old, ok := c.previous[a.ID]; ok && old.State == a.State && old.IPv4 == a.IPv4 && old.Gateway == a.Gateway && old.Index == a.Index && (a.Kind != "ethernet" || old.SpeedMbps == a.SpeedMbps) {
			a.LastChange = old.LastChange
		}

		c.previous[a.ID] = a
		adapters = append(adapters, a)
	}

	sort.Slice(adapters, func(i, j int) bool {
		return adapters[i].Name < adapters[j].Name
	})

	return adapters, nil
}
