package client_test

import (
	"testing"

	"github.com/sa1tbaby/go-network-stat/backend/api/client"
)

func TestBackendURLMustBeLocal(t *testing.T) {
	for _, address := range []string{"http://127.0.0.1:8765/", "http://127.0.0.1:8784"} {
		if !client.ValidBackendURL(address) {
			t.Errorf("rejected valid local URL: %s", address)
		}
	}

	for _, address := range []string{
		"https://127.0.0.1:8765/",
		"http://example.com:8765/",
		"http://localhost:8765/",
		"http://127.0.0.1:8765/evil",
		"http://user@127.0.0.1:8765/",
		"http://127.0.0.1:8765/?other=1",
		"http://127.0.0.1:65536/",
		"http://127.0.0.1/",
	} {
		if client.ValidBackendURL(address) {
			t.Errorf("accepted invalid backend URL: %s", address)
		}
	}
}
