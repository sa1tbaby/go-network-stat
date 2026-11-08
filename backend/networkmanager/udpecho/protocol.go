// Package udpecho implements the controlled CDXUDP01 echo protocol. Its results
// compare a round trip to an endpoint the user controls, not a game's protocol.
package udpecho

import (
	"bytes"
	"encoding/binary"
	"errors"
)

var magic = []byte("CDXUDP01")

type Header struct {
	Sequence uint32
	Tick     uint64
	Nonce    uint64
}

func Packet(size int, sequence uint32, tick, nonce uint64) ([]byte, error) {
	if size < 32 || size > 1200 {
		return nil, errors.New("controlled UDP payload must be within 32..1200 bytes")
	}

	b := make([]byte, size)
	copy(b, magic)
	binary.LittleEndian.PutUint32(b[8:12], 1)
	binary.LittleEndian.PutUint32(b[12:16], sequence)
	binary.LittleEndian.PutUint64(b[16:24], tick)
	binary.LittleEndian.PutUint64(b[24:32], nonce)

	return b, nil
}

func ParsePacket(b []byte) (Header, bool) {
	if len(b) < 32 || len(b) > 1200 || !bytes.Equal(b[:8], magic) || binary.LittleEndian.Uint32(b[8:12]) != 1 {
		return Header{}, false
	}

	return Header{
		Sequence: binary.LittleEndian.Uint32(b[12:16]),
		Tick:     binary.LittleEndian.Uint64(b[16:24]),
		Nonce:    binary.LittleEndian.Uint64(b[24:32]),
	}, true
}
