package udpecho_test

import (
	"encoding/binary"
	"fmt"
	"testing"
	"time"

	"github.com/sa1tbaby/go-network-stat/backend/networkmanager/udpecho"
)

func TestControlledUDPWireFormat(t *testing.T) {
	packet, err := udpecho.Packet(256, 0xA1B2C3D4, 0x0102030405060708, 0x1020304050607080)
	if err != nil {
		t.Fatal(err)
	}

	if string(packet[:8]) != "CDXUDP01" || binary.LittleEndian.Uint32(packet[8:12]) != 1 || packet[12] != 0xD4 || packet[16] != 8 || packet[24] != 0x80 {
		t.Fatalf("wire protocol changed: %x", packet[:32])
	}

	header, valid := udpecho.ParsePacket(packet)
	if !valid || header.Sequence != 0xA1B2C3D4 || header.Tick != 0x0102030405060708 || header.Nonce != 0x1020304050607080 {
		t.Fatalf("wire header decode mismatch: %+v %v", header, valid)
	}

	for _, size := range []int{0, 31, 1201} {
		if _, err = udpecho.Packet(size, 0, 0, 0); err == nil {
			t.Errorf("accepted invalid payload size%d", size)
		}
	}

	for _, size := range []int{0, 31, 1201} {
		if _, valid = udpecho.ParsePacket(make([]byte, size)); valid {
			t.Errorf("accepted invalid datagram size%d", size)
		}
	}

	packet[8] = 2
	if _, valid = udpecho.ParsePacket(packet); valid {
		t.Fatal("accepted wrong protocol version")
	}
}

func TestResponderRateLimitAndPeerMemoryBounds(t *testing.T) {
	now := time.Now()
	limiter := udpecho.RateLimiter{}
	for i := 0; i < 200; i++ {
		if !limiter.Allow("192.0.2.1", now) {
			t.Fatalf("rate limited packet%d below limit", i)
		}
	}

	if limiter.Allow("192.0.2.1", now) {
		t.Fatal("allowed more than200 echoes per second per IP")
	}

	if !limiter.Allow("192.0.2.2", now) {
		t.Fatal("one IP consumed another IP's allowance")
	}

	if !limiter.Allow("192.0.2.1", now.Add(time.Second)) {
		t.Fatal("rate limit did not reset at a new window")
	}

	bounded := udpecho.RateLimiter{}
	for i := 0; i < 1024; i++ {
		if !bounded.Allow(fmt.Sprint(i), now) {
			t.Fatal("peer limit reached early")
		}
	}

	if bounded.Allow("extra", now) {
		t.Fatal("unbounded responder peer state")
	}

	if !bounded.Allow("extra", now.Add(61*time.Second)) {
		t.Fatal("idle peer entries were not reclaimed")
	}
}
