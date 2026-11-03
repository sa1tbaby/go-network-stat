package collectors

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// ParseWLANStatistics decodes WLAN_STATISTICS, whose fixed header and MAC
// counters precede a variable-length array of WLAN_PHY_FRAME_STATISTICS.
// All bounds are checked before interpreting driver-provided bytes.
func ParseWLANStatistics(data []byte) ([]PHYStats, error) {
	if len(data) < 224 {
		return nil, errors.New("WLAN_STATISTICS is shorter than its fixed header")
	}

	count := binary.LittleEndian.Uint32(data[216:220])
	if count > 64 || uint64(count)*144+224 > uint64(len(data)) {
		return nil, fmt.Errorf("invalid WLAN PHY array bounds: %d PHYs in %d bytes", count, len(data))
	}

	phys := make([]PHYStats, 0, count)
	for i := uint32(0); i < count; i++ {
		b := data[224+int(i)*144 : 224+int(i+1)*144]
		values := [18]uint64{}
		for j := range values {
			values[j] = binary.LittleEndian.Uint64(b[j*8 : j*8+8])
		}

		phys = append(phys, PHYStats{
			Index:                  int(i),
			TxFrames:               values[0],
			MulticastTxFrames:      values[1],
			FailedFrames:           values[2],
			RetryFrames:            values[3],
			MultipleRetryFrames:    values[4],
			TxLifetimeExceeded:     values[5],
			TxFragments:            values[6],
			RTSSuccess:             values[7],
			RTSFailure:             values[8],
			ACKFailure:             values[9],
			RxFrames:               values[10],
			MulticastRxFrames:      values[11],
			PromiscuousRxFrames:    values[12],
			RxLifetimeExceeded:     values[13],
			DuplicateFrames:        values[14],
			RxFragments:            values[15],
			PromiscuousRxFragments: values[16],
			FCSErrors:              values[17],
		})
	}

	return phys, nil
}

func DuplicatePHYCounters(phys []PHYStats) bool {
	if len(phys) < 2 {
		return false
	}

	a := phys[0]
	for _, b := range phys[1:] {
		if a.TxFrames != b.TxFrames || a.RetryFrames != b.RetryFrames || a.MultipleRetryFrames != b.MultipleRetryFrames || a.FailedFrames != b.FailedFrames || a.ACKFailure != b.ACKFailure || a.FCSErrors != b.FCSErrors {
			return false
		}
	}

	return true
}
