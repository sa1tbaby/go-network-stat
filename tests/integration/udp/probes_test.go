package udp_test

import (
	"context"
	"encoding/binary"
	"net"
	"runtime"
	"testing"
	"time"

	"github.com/sa1tbaby/go-network-stat/backend/networkmanager/collectors"
)

func loopbackRequest(t *testing.T) collectors.ProbeRequest {
	t.Helper()
	index := 0
	if runtime.GOOS == "windows" {
		interfaces, err := net.Interfaces()
		if err != nil {
			t.Fatal(err)
		}

		for _, adapter := range interfaces {
			addresses, _ := adapter.Addrs()
			for _, address := range addresses {
				ip, _, _ := net.ParseCIDR(address.String())
				if ip.Equal(net.ParseIP("127.0.0.1")) {
					index = adapter.Index
				}
			}
		}

		if index == 0 {
			t.Fatal("no IPv4 loopback interface found")
		}
	}

	return collectors.ProbeRequest{
		Source:         "127.0.0.1",
		Address:        "127.0.0.1",
		InterfaceIndex: index,
		TimeoutMs:      1000,
	}
}

func TestTCPSourceBindingAndRefusalClassification(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	port := listener.Addr().(*net.TCPAddr).Port
	accepted := make(chan string, 1)
	go func() {
		connection, err := listener.Accept()
		if err == nil {
			accepted <- connection.RemoteAddr().(*net.TCPAddr).IP.String()
			connection.Close()
		}
	}()
	request := loopbackRequest(t)
	result := collectors.New().TCP(context.Background(), request, port)
	if !result.OK || result.ToolError || result.RTTMs == nil {
		t.Fatalf("TCP connection failed: %+v", result)
	}

	select {
	case source := <-accepted:
		if source != request.Source {
			t.Fatalf("wrong local source: %s", source)
		}
	case <-time.After(time.Second):
		t.Fatal("connection was not accepted")
	}

	listener.Close()
	result = collectors.New().TCP(context.Background(), request, port)
	if result.OK || result.ToolError || result.Status != "ConnectionRefused" {
		t.Fatalf("remote refusal must be a probe failure, not a local tool error: %+v", result)
	}
}

func TestDNSIgnoresUnrelatedDatagramAndRecordsRCODE(t *testing.T) {
	server, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}

	defer server.Close()
	go func() {
		buffer := make([]byte, 512)
		n, peer, err := server.ReadFromUDP(buffer)
		if err != nil {
			return
		}

		reply := append([]byte(nil), buffer[:n]...)
		reply[2] = 0x81
		reply[3] = 0x83
		binary.BigEndian.PutUint16(reply, binary.BigEndian.Uint16(reply)+1)
		server.WriteToUDP(reply, peer)
		binary.BigEndian.PutUint16(reply, binary.BigEndian.Uint16(reply)-1)
		server.WriteToUDP(reply, peer)
	}()
	collector := collectors.New()
	request := loopbackRequest(t)
	request.DNSPort = server.LocalAddr().(*net.UDPAddr).Port
	result := collector.DNS(context.Background(), request)
	if !result.OK || result.Status != "Success_DNS_RCODE_3" || result.Bytes != 29 || result.RTTMs == nil {
		t.Fatalf("DNS response was not correctly correlated: %+v", result)
	}
}

func TestDNSOwnerCancellationInterruptsRead(t *testing.T) {
	server, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}

	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	request := loopbackRequest(t)
	request.TimeoutMs = 5000
	go func() {
		buffer := make([]byte, 512)
		server.ReadFromUDP(buffer)
		cancel()
	}()
	started := time.Now()
	request.DNSPort = server.LocalAddr().(*net.UDPAddr).Port
	result := collectors.New().DNS(ctx, request)
	if result.Status != "Cancelled" || !result.ToolError || time.Since(started) > time.Second {
		t.Fatalf("owner cancellation was counted as network loss or did not interrupt read: %+v", result)
	}
}
