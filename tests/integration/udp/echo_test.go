package udp_test

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/sa1tbaby/go-network-stat/backend/networkmanager/udpecho"
)

func loopbackConfig(t *testing.T, port int) udpecho.TestConfig {
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
			t.Fatal("no IPv4 loopback adapter")
		}
	}

	return udpecho.TestConfig{
		Server:         "127.0.0.1",
		Port:           port,
		Source:         "127.0.0.1",
		InterfaceIndex: index,
		AdapterName:    "Loopback",
		PPS:            30,
		PayloadBytes:   256,
		Duration:       150 * time.Millisecond,
		LogDirectory:   t.TempDir(),
	}
}

func TestControlledUDPClientResponderAndReport(t *testing.T) {
	server, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- udpecho.Serve(ctx, server, 0, io.Discard)
	}()
	cfg := loopbackConfig(t, server.LocalAddr().(*net.UDPAddr).Port)
	path, err := udpecho.Test(context.Background(), cfg, io.Discard)
	if err != nil {
		t.Fatal(err)
	}

	cancel()
	select {
	case err = <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("responder did not stop with its owner")
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	var report map[string]any
	if err = json.Unmarshal(content, &report); err != nil {
		t.Fatal(err)
	}

	if report["handshakeVerified"] != true || report["sentDatagrams"].(float64) < 1 || report["timelyEchoes"] != report["sentDatagrams"] || report["echoFailuresByDeadline"] != float64(0) || report["echoFailurePct"] != float64(0) {
		t.Fatalf("controlled echo test report incorrect: %s", content)
	}

	content, err = os.ReadFile(filepath.Join(cfg.LogDirectory, "probes.csv"))
	if err != nil {
		t.Fatal(err)
	}

	rows, err := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(content, []byte{0xEF, 0xBB, 0xBF}))).ReadAll()
	if err != nil {
		t.Fatal(err)
	}

	if len(rows) < 2 || len(rows[0]) != 9 || rows[0][0] != "event_time" || rows[1][3] != "Success" {
		t.Fatalf("CSV protocol changed: %+v", rows)
	}
}

func TestFailedHandshakeIsUnavailableAndNeverHundredPercentLoss(t *testing.T) {
	server, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}

	defer server.Close()
	go func() {
		buffer := make([]byte, 1200)
		n, peer, err := server.ReadFromUDP(buffer)
		if err == nil {
			buffer[0] = 'X'
			server.WriteToUDP(buffer[:n], peer)
		}
	}()
	path, err := udpecho.Test(context.Background(), loopbackConfig(t, server.LocalAddr().(*net.UDPAddr).Port), io.Discard)
	if err == nil {
		t.Fatal("started measurements without verifying controlled responder")
	}

	content, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}

	var report map[string]any
	if err = json.Unmarshal(content, &report); err != nil {
		t.Fatal(err)
	}

	if report["handshakeVerified"] != false || report["sentDatagrams"] != float64(0) || report["echoFailurePct"] != nil || report["echoFailuresByDeadline"] != float64(0) || report["error"] == nil {
		t.Fatalf("unverified endpoint was reported as measured packet loss: %s", content)
	}
}

func TestClientStopDrainsSentEchoWithoutSendingAnother(t *testing.T) {
	server, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}

	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		buffer := make([]byte, 1200)
		n, peer, err := server.ReadFromUDP(buffer)
		if err != nil {
			return
		}

		server.WriteToUDP(buffer[:n], peer)
		_, _, err = server.ReadFromUDP(buffer)
		if err == nil {
			cancel()
		}
	}()
	cfg := loopbackConfig(t, server.LocalAddr().(*net.UDPAddr).Port)
	cfg.Duration = 2 * time.Second
	path, err := udpecho.Test(ctx, cfg, io.Discard)
	if err != nil {
		t.Fatal(err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	var report map[string]any
	if err = json.Unmarshal(content, &report); err != nil {
		t.Fatal(err)
	}

	if report["stoppedByUser"] != true || report["sentDatagrams"] != float64(1) || report["pendingAtExit"] != float64(0) || report["completedProbes"] != float64(1) {
		t.Fatalf("stop failed to drain the already-sent probe or sent another after stop: %s", content)
	}
}
