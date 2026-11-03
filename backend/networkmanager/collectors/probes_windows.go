//go:build windows

package collectors

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math/bits"
	"net"
	"runtime"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

var icmpCreateFile = ipHelper.NewProc("IcmpCreateFile")

var icmpCloseHandle = ipHelper.NewProc("IcmpCloseHandle")

var icmpSendEcho2Ex = ipHelper.NewProc("IcmpSendEcho2Ex")

var probeSequence atomic.Uint64

func socketControl(index int) func(string, string, syscall.RawConn) error {
	return func(network, address string, connection syscall.RawConn) error {
		if index <= 0 {
			return errors.New("source-bound probe requires a positive interface index")
		}

		var optionError error
		err := connection.Control(func(fd uintptr) {
			// IP_UNICAST_IF takes the interface index in network byte order.
			optionError = syscall.SetsockoptInt(syscall.Handle(fd), syscall.IPPROTO_IP, 31, int(bits.ReverseBytes32(uint32(index))))
		})
		if err != nil {
			return err
		}

		return optionError
	}
}

func socketError(err error) (string, bool, bool) {
	var errno syscall.Errno
	if !errors.As(err, &errno) {
		return "", false, false
	}

	switch errno {
	case 10060:
		return "TimedOut", false, true
	case 10061:
		return "ConnectionRefused", false, true
	case 10054:
		return "ConnectionReset", false, true
	case 10051:
		return "NetworkUnreachable", false, true
	case 10065:
		return "HostUnreachable", false, true
	case 10050:
		return "NetworkDown", false, true
	case 10053:
		return "ConnectionAborted", false, true
	case 10013:
		return "AccessDenied", true, true
	case 10022:
		return "InvalidArgument", true, true
	case 10049:
		return "SourceAddressUnavailable", true, true
	case 10055:
		return "LocalBufferExhausted", true, true
	default:
		return "SocketError: " + errno.Error(), true, true
	}
}

func validateICMPSource(request ProbeRequest) error {
	if request.InterfaceIndex <= 0 {
		return errors.New("ICMP requires a positive interface index")
	}

	safe, err := strongHost(uint32(request.InterfaceIndex))
	if err != nil {
		return err
	}

	if !safe {
		return errors.New("ICMP cannot be attributed to this interface because WeakHostSend is enabled")
	}

	return validateSocketSource(request)
}

func validateSocketSource(request ProbeRequest) error {
	if request.InterfaceIndex <= 0 {
		return errors.New("source-bound probe requires a positive interface index")
	}

	adapter, err := net.InterfaceByIndex(request.InterfaceIndex)
	if err != nil {
		return err
	}

	addresses, err := adapter.Addrs()
	if err != nil {
		return err
	}

	for _, address := range addresses {
		ip, _, err := net.ParseCIDR(address.String())
		if err == nil && ip.Equal(net.ParseIP(request.Source)) {
			return nil
		}
	}

	return errors.New("selected source IPv4 no longer belongs to the selected interface")
}

func icmpStatus(code uint32) (string, bool) {
	switch code {
	case 0:
		return "Success", false
	case 11010:
		return "TimedOut", false
	case 11002:
		return "NetworkUnreachable", false
	case 11003:
		return "HostUnreachable", false
	case 11004:
		return "ProtocolUnreachable", false
	case 11005:
		return "PortUnreachable", false
	case 11009:
		return "PacketTooBig", false
	case 11012:
		return "BadRoute", false
	case 11013:
		return "TtlExpired", false
	case 11014:
		return "TtlReassemblyExpired", false
	case 5, 6, 8, 50, 87, 11001, 11006, 11007, 11011, 11015, 11017, 11018:
		return fmt.Sprintf("ICMP_%d", code), true
	default:
		return fmt.Sprintf("ICMP_%d", code), false
	}
}

func (c *nativeCollector) ICMP(ctx context.Context, request ProbeRequest) ProbeResult {
	src, dst, err := probeIPs(request)
	if err != nil {
		return failedProbe(err, request.PayloadBytes)
	}

	if err = ctx.Err(); err != nil {
		return failedProbe(err, request.PayloadBytes)
	}

	if err = validateICMPSource(request); err != nil {
		return failedProbe(err, request.PayloadBytes)
	}

	size := request.PayloadBytes
	if size <= 0 {
		size = 32
	}

	if size < 8 || size > 65000 {
		return failedProbe(errors.New("ICMP payload must be within 8..65000 bytes"), size)
	}

	ttl := request.TTL
	if ttl == 0 {
		ttl = 128
	}

	if ttl < 1 || ttl > 255 {
		return failedProbe(errors.New("ICMP TTL must be within 1..255"), size)
	}

	ctx, cancel := probeContext(ctx, request)
	defer cancel()
	if err = ctx.Err(); err != nil {
		return failedProbe(err, size)
	}

	handle, _, callErr := icmpCreateFile.Call()
	if handle == 0 || handle == ^uintptr(0) {
		return failedProbe(fmt.Errorf("IcmpCreateFile: %w", callErr), size)
	}

	defer icmpCloseHandle.Call(handle)
	payload := make([]byte, size)
	binary.LittleEndian.PutUint64(payload, probeSequence.Add(1))
	// More than the documented ICMP_ECHO_REPLY + payload + error allowance.
	reply := make([]byte, size+1024)
	options := [16]byte{}
	options[0] = byte(ttl)
	if size > 32 {
		options[2] = 2
	} // IP_FLAG_DF for diagnostic payload-size comparisons.
	deadline, _ := ctx.Deadline()
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return failedProbe(context.DeadlineExceeded, size)
	}

	timeout := uint32((remaining + time.Millisecond - 1) / time.Millisecond)
	count, _, callErr := icmpSendEcho2Ex.Call(handle, 0, 0, 0, uintptr(binary.LittleEndian.Uint32(src)), uintptr(binary.LittleEndian.Uint32(dst)), uintptr(unsafe.Pointer(&payload[0])), uintptr(len(payload)), uintptr(unsafe.Pointer(&options[0])), uintptr(unsafe.Pointer(&reply[0])), uintptr(len(reply)), uintptr(timeout))
	runtime.KeepAlive(payload)
	runtime.KeepAlive(reply)
	runtime.KeepAlive(options)
	result := ProbeResult{Bytes: size, Received: count > 0}
	if count == 0 {
		var errno syscall.Errno
		if !errors.As(callErr, &errno) {
			return failedProbe(fmt.Errorf("IcmpSendEcho2Ex: %v", callErr), size)
		}

		if errno == 0 {
			return failedProbe(errors.New("IcmpSendEcho2Ex returned neither a reply nor an error status"), size)
		}

		result.IPStatus = uint32(errno)
	} else {
		result.IPStatus = binary.LittleEndian.Uint32(reply[4:8])
		result.ReplyAddress = net.IP(reply[:4]).String()
		rtt := float64(binary.LittleEndian.Uint32(reply[8:12]))
		result.RTTMs = &rtt
	}

	result.Status, result.ToolError = icmpStatus(result.IPStatus)
	result.OK = result.Received && result.IPStatus == 0
	if ctx.Err() != nil && ctx.Err() != context.DeadlineExceeded {
		return failedProbe(ctx.Err(), size)
	}

	return result
}
