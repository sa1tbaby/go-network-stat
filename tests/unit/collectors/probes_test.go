package collectors_test

import (
	"context"
	"testing"

	"github.com/sa1tbaby/go-network-stat/backend/networkmanager/collectors"
)

func TestDNSReplyMustMatchTransactionAndQuestion(t *testing.T) {
	const transaction = uint16(0x12AB)
	valid := collectors.DNSQuery(transaction)
	valid[2] = 0x81
	valid[3] = 0x83 // NXDOMAIN still demonstrates a responding DNS peer.
	if !collectors.ValidDNSReply(valid, transaction) {
		t.Fatal("rejected a matching DNS response with a nonzero RCODE")
	}

	cases := map[string]func([]byte) []byte{
		"truncated": func(b []byte) []byte {
			return b[:28]
		},
		"wrong_id": func(b []byte) []byte {
			b[0]++

			return b
		},
		"query_not_reply": func(b []byte) []byte {
			b[2] &^= 0x80

			return b
		},
		"wrong_opcode": func(b []byte) []byte {
			b[2] |= 0x08

			return b
		},
		"wrong_question_count": func(b []byte) []byte {
			b[5] = 2

			return b
		},
		"wrong_question": func(b []byte) []byte {
			b[13] = 'z'

			return b
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			if collectors.ValidDNSReply(change(append([]byte(nil), valid...)), transaction) {
				t.Fatal("accepted an unrelated/invalid UDP reply")
			}
		})
	}
}

func TestInvalidSourceIsToolError(t *testing.T) {
	for _, request := range []collectors.ProbeRequest{
		{Source: "", Address: "1.1.1.1"},
		{Source: "0.0.0.0", Address: "1.1.1.1"},
		{Source: "127.0.0.1", Address: "not-an-ip"},
		{Source: "::1", Address: "1.1.1.1"},
	} {
		if result := collectors.New().TCP(context.Background(), request, 443); result.OK || !result.ToolError {
			t.Fatalf("invalid local probe input counted as loss: %+v", result)
		}
	}
}
