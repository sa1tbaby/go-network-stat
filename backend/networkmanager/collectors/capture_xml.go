package collectors

import (
	"encoding/binary"
	"encoding/xml"
	"errors"
	"net"
	"strconv"
	"strings"
	"time"
)

type kernelNetworkXML struct {
	System struct {
		Provider struct {
			GUID string `xml:"Guid,attr"`
			Name string `xml:"Name,attr"`
		} `xml:"Provider"`
		EventID int `xml:"EventID"`
		Time    struct {
			SystemTime string `xml:"SystemTime,attr"`
		} `xml:"TimeCreated"`
	} `xml:"System"`
	Data struct {
		Values []struct {
			Name  string `xml:"Name,attr"`
			Value string `xml:",chardata"`
		} `xml:"Data"`
	} `xml:"EventData"`
}

// ParseKernelNetworkXML uses payload PID rather than the event-header PID (which
// can be System). Values are the manifest's raw UInt32/UInt16 representations.
func ParseKernelNetworkXML(data []byte, gamePID int) (*UDPEvent, bool, error) {
	var value kernelNetworkXML
	if err := xml.Unmarshal(data, &value); err != nil {
		return nil, false, err
	}

	provider := strings.Trim(strings.ToLower(value.System.Provider.GUID), "{}")
	if provider != "7dd42a49-5329-4832-8dfd-43d979153a88" {
		return nil, false, nil
	}

	id := value.System.EventID
	if id != 42 && id != 43 && id != 58 && id != 59 {
		return nil, false, nil
	}

	fields := map[string]string{}
	for _, field := range value.Data.Values {
		fields[strings.ToLower(field.Name)] = strings.TrimSpace(field.Value)
	}

	pid, err := strconv.ParseInt(fields["pid"], 0, 64)
	if err != nil || int(pid) != gamePID {
		return nil, false, nil
	}

	if id == 58 || id == 59 {
		return nil, true, nil
	}

	at, err := time.Parse(time.RFC3339Nano, value.System.Time.SystemTime)
	if err != nil {
		return nil, false, err
	}

	parseAddress := func(text string) (string, error) {
		if ip := net.ParseIP(text).To4(); ip != nil {
			return ip.String(), nil
		}

		number, parseErr := strconv.ParseUint(text, 0, 32)
		if parseErr != nil {
			return "", parseErr
		}

		var octets [4]byte
		binary.LittleEndian.PutUint32(octets[:], uint32(number))

		return net.IP(octets[:]).String(), nil
	}
	destination, err := parseAddress(fields["daddr"])
	if err != nil {
		return nil, false, err
	}

	source, err := parseAddress(fields["saddr"])
	if err != nil {
		return nil, false, err
	}

	destinationRawPort, err := strconv.ParseUint(fields["dport"], 0, 16)
	if err != nil {
		return nil, false, err
	}

	sourceRawPort, err := strconv.ParseUint(fields["sport"], 0, 16)
	if err != nil {
		return nil, false, err
	}

	size, err := strconv.ParseInt(fields["size"], 0, 32)
	if err != nil || size < 0 {
		return nil, false, errors.New("Invalid ETW payload size")
	}

	destinationPort := int((destinationRawPort&255)<<8 | destinationRawPort>>8)
	sourcePort := int((sourceRawPort&255)<<8 | sourceRawPort>>8)
	event := &UDPEvent{
		PID:      gamePID,
		UnixMs:   at.UnixMilli(),
		Outgoing: id == 42,
		Size:     int(size),
	}
	if event.Outgoing {
		event.LocalIP, event.LocalPort, event.RemoteIP, event.RemotePort = source, sourcePort, destination, destinationPort
	} else {
		event.LocalIP, event.LocalPort, event.RemoteIP, event.RemotePort = destination, destinationPort, source, sourcePort
	}

	return event, false, nil
}
