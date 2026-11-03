package udp_test

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/sa1tbaby/go-network-stat/backend/networkmanager/collectors"
)

func TestDNSStreamMatchesLateReplyWithoutDoubleCountingTimeout(t *testing.T) {
	server, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}

	defer server.Close()
	var serverWorkers sync.WaitGroup
	serverWorkers.Add(1)
	peers := make(chan string, 10)
	go func() {
		defer serverWorkers.Done()
		buffer := make([]byte, 512)
		first := true
		for {
			n, peer, err := server.ReadFromUDP(buffer)
			if err != nil {
				return
			}

			peers <- peer.String()
			reply := append([]byte(nil), buffer[:n]...)
			reply[2] = 0x81
			reply[3] = 0x80
			delay := time.Duration(0)
			if first {
				delay = 180 * time.Millisecond
				first = false
			}

			serverWorkers.Add(1)
			go func(reply []byte, peer *net.UDPAddr, delay time.Duration) {
				defer serverWorkers.Done()
				time.Sleep(delay)
				server.WriteToUDP(reply, peer)
			}(reply, peer, delay)
		}
	}()
	request := loopbackRequest(t)
	request.DNSPort = server.LocalAddr().(*net.UDPAddr).Port
	request.TimeoutMs = 80
	request.DurationMs = 350
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var observations []collectors.DNSObservation
	err = collectors.New().(collectors.DNSStreamCollector).StreamDNS(ctx, request, 10, func(observation collectors.DNSObservation) {
		observations = append(observations, observation)
	})
	if err != nil {
		t.Fatal(err)
	}

	server.Close()
	serverWorkers.Wait()
	close(peers)
	completed, good, timeouts, late := 0, 0, 0, 0
	for _, observation := range observations {
		if observation.Late {
			late++
			if !observation.Sent.IsZero() || observation.Result.Status != "LateReplyAfterTimeout" {
				t.Fatalf("late observation has an original send and could be counted twice: %+v", observation)
			}

			continue
		}

		completed++
		if observation.Result.OK {
			good++
		}

		if observation.Result.Status == "TimedOut" {
			timeouts++
		}
	}

	if completed != 4 || good != 3 || timeouts != 1 || late != 1 {
		t.Fatalf("completed=%d good=%d timeouts=%d late=%d; observations=%+v", completed, good, timeouts, late, observations)
	}

	peer := ""
	for current := range peers {
		if peer == "" {
			peer = current
		}

		if current != peer {
			t.Fatalf("DNS stream changed source UDP socket: %s to %s", peer, current)
		}
	}
}

func TestDNSStreamCancellationDoesNotCreateFailedProbe(t *testing.T) {
	server, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}

	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		buffer := make([]byte, 512)
		server.ReadFromUDP(buffer)
		cancel()
	}()
	request := loopbackRequest(t)
	request.DNSPort = server.LocalAddr().(*net.UDPAddr).Port
	request.TimeoutMs = 5000
	observed := 0
	started := time.Now()
	err = collectors.New().(collectors.DNSStreamCollector).StreamDNS(ctx, request, 1, func(observation collectors.DNSObservation) {
		observed++
	})
	if err != nil || observed != 0 || time.Since(started) > time.Second {
		t.Fatalf("owner cancellation produced a failed network probe or did not stop: err=%v observations=%d", err, observed)
	}
}

func TestDNSStreamDrainsFinalRequestAfterSendDuration(t *testing.T) {
	server, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}

	defer server.Close()
	request := loopbackRequest(t)
	request.DNSPort = server.LocalAddr().(*net.UDPAddr).Port
	request.TimeoutMs = 80
	request.DurationMs = 1
	pending := []int{}
	request.OnPending = func(count int) {
		pending = append(pending, count)
	}
	var observations []collectors.DNSObservation
	started := time.Now()
	err = collectors.New().(collectors.DNSStreamCollector).StreamDNS(context.Background(), request, 1, func(observation collectors.DNSObservation) {
		observations = append(observations, observation)
	})
	if err != nil || len(observations) != 1 || observations[0].Result.Status != "TimedOut" || time.Since(started) < 80*time.Millisecond {
		t.Fatalf("final DNS request was not drained to its deadline: err=%v observations=%+v", err, observations)
	}

	maximum := 0
	for _, count := range pending {
		if count > maximum {
			maximum = count
		}
	}

	if maximum != 1 || len(pending) == 0 || pending[len(pending)-1] != 0 {
		t.Fatalf("pending telemetry did not track/drain in-flight probe: %v", pending)
	}
}
