//go:build windows

package collectors_test

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/sa1tbaby/go-network-stat/backend/networkmanager/collectors"
)

func TestNativePhysicalAdaptersHaveConsistentSourceIdentity(t *testing.T) {
	adapters, err := collectors.New().Adapters(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	for _, adapter := range adapters {
		if adapter.ID == "" || adapter.Name == "" || adapter.State == "" || (adapter.Kind != "wifi" && adapter.Kind != "ethernet") {
			t.Fatalf("invalid physical adapter: %+v", adapter)
		}

		if adapter.Available && (adapter.State != "Up" || adapter.Index <= 0 || net.ParseIP(adapter.IPv4).To4() == nil) {
			t.Fatalf("available adapter lacks a usable source identity: %+v", adapter)
		}

		if len(adapter.InterfaceGUID) != 36 {
			t.Fatalf("invalid native interface GUID: %q", adapter.InterfaceGUID)
		}
	}
}

func TestCanceledNativeCollectionDoesNotStartProbe(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	collector := collectors.New()
	if _, err := collector.Adapters(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("adapter read ignored cancellation: %v", err)
	}

	result := collector.ICMP(ctx, collectors.ProbeRequest{
		Source:         "127.0.0.1",
		Address:        "127.0.0.1",
		InterfaceIndex: 1,
	})
	if result.OK || !result.ToolError || result.Status != "Cancelled" {
		t.Fatalf("owner cancellation reported as network loss: %+v", result)
	}

	if _, err := collector.Radio(ctx, collectors.Adapter{Kind: "wifi"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("radio read ignored cancellation: %v", err)
	}
}
