package collectors_test

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sa1tbaby/go-network-stat/backend/networkmanager/collectors"
)

func classifierFlow(pid int, address string, port, count int, start int64) []collectors.UDPEvent {
	rows := make([]collectors.UDPEvent, 0, count)
	for i := 0; i < count; i++ {
		rows = append(rows, collectors.UDPEvent{
			PID:        pid,
			UnixMs:     start + int64(i)*19000/int64(count-1),
			Outgoing:   i%2 == 0,
			LocalIP:    "192.168.1.2",
			LocalPort:  50000,
			RemoteIP:   address,
			RemotePort: port,
			Size:       256,
		})
	}

	return rows
}

func TestUDPClassifierRequiresSustainedDominantBidirectionalFlow(t *testing.T) {
	start := int64(1_700_000_000_000)
	rows := classifierFlow(42, "8.8.4.4", 3074, 400, start)
	result := collectors.ClassifyUDP(rows, 42, start, start+20000, "cod")
	if result.State != "detected" || result.Selected == nil || result.Selected.RemotePort != 3074 || result.Selected.BidirectionalWindows != 4 {
		t.Fatalf("unexpected analysis: %#v", result)
	}

	rows = append(rows, classifierFlow(42, "9.9.9.9", 40000, 300, start)...)
	result = collectors.ClassifyUDP(rows, 42, start, start+20000, "cod")
	if result.State != "ambiguous" || result.Selected != nil {
		t.Fatalf("ambiguous flows selected a peer: %#v", result)
	}
}

func TestUDPClassifierRejectsShortStaleOneWayAndServiceTraffic(t *testing.T) {
	start := int64(100000)
	for _, test := range []struct {
		name string
		rows []collectors.UDPEvent
	}{
		{"short", classifierFlow(42, "8.8.4.4", 3074, 100, start)},
		{"stale", classifierFlow(42, "8.8.4.4", 3074, 400, start-10000)},
		{"quic", classifierFlow(42, "8.8.4.4", 443, 1000, start)},
		{"private", classifierFlow(42, "192.168.1.1", 3074, 1000, start)},
		{"different-pid", classifierFlow(99, "8.8.4.4", 3074, 400, start)},
	} {
		t.Run(test.name, func(t *testing.T) {
			analysis := collectors.ClassifyUDP(test.rows, 42, start, start+20000, "cs2")
			if analysis.State != "no_active_flow" || analysis.Selected != nil {
				t.Fatalf("false positive: %#v", analysis)
			}
		})
	}

	rows := classifierFlow(42, "8.8.4.4", 3074, 1000, start)
	for i := range rows {
		rows[i].Outgoing = true
	}

	if collectors.ClassifyUDP(rows, 42, start, start+20000, "cod").State != "no_active_flow" {
		t.Fatal("one-way flow accepted")
	}
}

func TestPublicIPv4RejectsUnroutableAndFakeIPRanges(t *testing.T) {
	for _, address := range []string{
		"0.0.0.1",
		"10.0.0.1",
		"100.64.0.1",
		"127.0.0.1",
		"169.254.1.1",
		"172.16.0.1",
		"192.168.0.1",
		"198.18.0.1",
		"224.0.0.1",
		"255.255.255.255",
		"::1",
		"bad",
	} {
		if collectors.PublicIPv4(address) {
			t.Errorf("accepted %s", address)
		}
	}

	if !collectors.PublicIPv4("8.8.4.4") {
		t.Fatal("public IPv4 rejected")
	}
}

func TestKernelNetworkXMLUsesPayloadPIDAndNetworkByteOrder(t *testing.T) {
	ip := func(text string) uint32 {
		return binary.LittleEndian.Uint32(net.ParseIP(text).To4())
	}
	at := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	xml := fmt.Sprintf(`<Event xmlns="http://schemas.microsoft.com/win/2004/08/events/event"><System><Provider Name="Microsoft-Windows-Kernel-Network" Guid="{7dd42a49-5329-4832-8dfd-43d979153a88}"/><EventID>42</EventID><TimeCreated SystemTime="%s"/><Execution ProcessID="4"/></System><EventData><Data Name="PID">84</Data><Data Name="size">256</Data><Data Name="daddr">%d</Data><Data Name="saddr">%d</Data><Data Name="dport">524</Data><Data Name="sport">20675</Data></EventData></Event>`, at.Format(time.RFC3339Nano), ip("8.8.4.4"), ip("192.168.1.2"))
	event, ipv6, err := collectors.ParseKernelNetworkXML([]byte(xml), 84)
	if err != nil || ipv6 || event == nil || event.PID != 84 || event.RemoteIP != "8.8.4.4" || event.LocalIP != "192.168.1.2" || event.RemotePort != 3074 || event.LocalPort != 50000 || event.UnixMs != at.UnixMilli() {
		t.Fatalf("parse result: %#v ipv6=%v err=%v", event, ipv6, err)
	}

	event, _, err = collectors.ParseKernelNetworkXML([]byte(xml), 4)
	if err != nil || event != nil {
		t.Fatal("event header PID was used instead of payload PID")
	}

	received := strings.Replace(xml, "<EventID>42</EventID>", "<EventID>43</EventID>", 1)
	event, _, err = collectors.ParseKernelNetworkXML([]byte(received), 84)
	if err != nil || event == nil || event.Outgoing || event.RemoteIP != "192.168.1.2" || event.RemotePort != 50000 {
		t.Fatalf("receive direction parse: %#v %v", event, err)
	}

	version6 := strings.Replace(xml, "<EventID>42</EventID>", "<EventID>58</EventID>", 1)
	event, ipv6, err = collectors.ParseKernelNetworkXML([]byte(version6), 84)
	if err != nil || event != nil || !ipv6 {
		t.Fatal("IPv6 evidence not retained separately")
	}
}

func TestCaptureReportRequiresMatchingRequestIdentity(t *testing.T) {
	dir := t.TempDir()
	request := collectors.CaptureRequest{
		AppID:           "codex-cod-relay-request",
		Game:            "cs2",
		RunID:           strings.Repeat("a", 32),
		GamePID:         42,
		GameStartTicks:  638000000000000001,
		OwnerPID:        99,
		OwnerStartTicks: 638000000000000003,
	}
	if err := collectors.WriteCaptureRequest(dir, request); err != nil {
		t.Fatal(err)
	}

	report := collectors.CaptureReport{
		AppID:          "codex-cod-relay-capture",
		Game:           request.Game,
		RunID:          request.RunID,
		GamePID:        request.GamePID,
		GameStartTicks: request.GameStartTicks,
		State:          "detected",
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dir, "capture.json"), encoded, 0600); err != nil {
		t.Fatal(err)
	}

	if _, err := collectors.ReadCaptureReport(dir, request); err != nil {
		t.Fatal(err)
	}

	request.GameStartTicks++
	if _, err := collectors.ReadCaptureReport(dir, request); err == nil {
		t.Fatal("accepted result for reused PID")
	}

	if err := collectors.CancelCapture(dir, request.RunID); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(dir, "cancel.json")); err != nil {
		t.Fatal(err)
	}
}
