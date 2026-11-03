package collectors_test

import (
	"encoding/binary"
	"testing"

	"github.com/sa1tbaby/go-network-stat/backend/networkmanager/collectors"
)

func TestWLANStatisticsBoundsAndCounterOffsets(t *testing.T) {
	for _, size := range []int{0, 223} {
		if _, err := collectors.ParseWLANStatistics(make([]byte, size)); err == nil {
			t.Fatalf("accepted %d-byte header", size)
		}
	}

	data := make([]byte, 224+2*144)
	binary.LittleEndian.PutUint32(data[216:], 2)
	for i := 0; i < 36; i++ {
		binary.LittleEndian.PutUint64(data[224+i*8:], uint64(1000+i))
	}

	phys, err := collectors.ParseWLANStatistics(data)
	if err != nil {
		t.Fatal(err)
	}

	if len(phys) != 2 || phys[0].TxFrames != 1000 || phys[0].FailedFrames != 1002 || phys[0].ACKFailure != 1009 || phys[0].FCSErrors != 1017 || phys[1].TxFrames != 1018 || phys[1].FCSErrors != 1035 {
		t.Fatalf("incorrect PHY layout: %+v", phys)
	}

	binary.LittleEndian.PutUint32(data[216:], 3)
	if _, err = collectors.ParseWLANStatistics(data); err == nil {
		t.Fatal("accepted PHY array beyond driver buffer")
	}

	binary.LittleEndian.PutUint32(data[216:], ^uint32(0))
	if _, err = collectors.ParseWLANStatistics(data); err == nil {
		t.Fatal("accepted overflowing driver PHY count")
	}
}

func TestDuplicatePHYCounterDetection(t *testing.T) {
	first := collectors.PHYStats{
		TxFrames:     100,
		RetryFrames:  10,
		FailedFrames: 2,
		ACKFailure:   4,
		FCSErrors:    7,
	}
	second := first
	second.Index = 1
	if collectors.DuplicatePHYCounters([]collectors.PHYStats{first}) {
		t.Fatal("one PHY cannot be duplicated")
	}

	if !collectors.DuplicatePHYCounters([]collectors.PHYStats{first, second}) {
		t.Fatal("duplicate driver counters not detected")
	}

	second.FCSErrors++
	if collectors.DuplicatePHYCounters([]collectors.PHYStats{first, second}) {
		t.Fatal("distinct driver PHYs incorrectly treated as identical")
	}
}
