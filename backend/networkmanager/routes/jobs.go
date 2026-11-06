package routes

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/sa1tbaby/go-network-stat/backend/networkmanager/storage"
)

func (engine *Engine) Status(adapter, address string) any {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	if job := engine.routes[adapter+"|"+address]; job != nil {
		encodedJob, _ := json.Marshal(job)
		var copy any
		_ = json.Unmarshal(encodedJob, &copy)

		return copy
	}

	return map[string]any{
		"state":              "idle",
		"adapterId":          adapter,
		"address":            address,
		"hops":               []any{},
		"destinationReached": false,
		"conclusion":         "Click Test route to start a separate series of TTL probes.",
	}
}

func (engine *Engine) Start(adapterID, address string) (any, error) {
	adapter, ok := engine.host.Adapter(adapterID)
	if !ok || !adapter.Available {
		return nil, errors.New("The selected connection is currently unavailable.")
	}

	if !adapter.ICMPSafe {
		return nil, errors.New("ICMP cannot be reliably bound to this interface because WeakHostSend is enabled.")
	}

	if !engine.host.HasICMPTarget(adapterID, address) {
		return nil, errors.New("First add this IPv4 address to the ICMP targets for the selected connection.")
	}

	dir := filepath.Join(engine.host.LogDirectory(), "routes")
	engine.mu.Lock()
	if engine.closed || engine.ctx.Err() != nil {
		engine.mu.Unlock()

		return nil, errors.New("The collector is shutting down.")
	}

	key := adapterID + "|" + address
	if old := engine.routes[key]; old != nil && old.State == "running" {
		engine.mu.Unlock()

		return map[string]any{"ok": true, "reused": true}, nil
	}

	active := 0
	for _, job := range engine.routes {
		if job.State == "running" {
			active++
		}
	}

	if active >= 2 {
		engine.mu.Unlock()

		return nil, errors.New("Up to two route tests can run at a time.")
	}

	if err := storage.NoLink(dir); err != nil {
		engine.mu.Unlock()

		return nil, err
	}

	if err := os.MkdirAll(dir, 0700); err != nil {
		engine.mu.Unlock()

		return nil, err
	}

	stamp := iso()
	job := &Job{
		AppID:      "codex-internet-stability-route",
		State:      "running",
		Phase:      "discovery",
		AdapterID:  adapterID,
		Address:    address,
		source:     adapter.IPv4,
		index:      adapter.Index,
		StartedAt:  stamp,
		UpdatedAt:  stamp,
		Conclusion: "Discovering responding hops…",
		LogFile:    filepath.Join(dir, fmt.Sprintf("%s-if%d-%s.json", time.Now().UTC().Format("20060102-150405.000000000"), adapter.Index, address)),
		Hops:       []*Hop{},
		SampleGoal: engine.options.SampleGoal,
		TimeoutMs:  engine.options.TimeoutMs,
		MaxHops:    engine.options.MaxHops,
	}
	if err := storage.PublishJSON(job.LogFile, job); err != nil {
		engine.mu.Unlock()

		return nil, err
	}

	engine.routes[key] = job
	engine.wg.Add(1)
	engine.workers++
	engine.mu.Unlock()
	engine.host.Event("route_start", adapterID+" → "+address)
	go func() {
		defer engine.wg.Done()
		defer func() {
			engine.mu.Lock()
			engine.workers--
			engine.mu.Unlock()
		}()
		engine.runRoute(job)
	}()

	return map[string]any{"ok": true, "reused": false}, nil
}

func (engine *Engine) saveRoute(job *Job) error {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	engine.assessRouteLocked(job)

	return storage.PublishJSON(job.LogFile, job)
}

func (engine *Engine) runRoute(job *Job) {
	err := engine.collectRoute(job)
	engine.mu.Lock()
	job.UpdatedAt = iso()
	kind := "route_complete"
	message := job.AdapterID + " → " + job.Address + ": " + job.Conclusion
	if err != nil {
		job.State = "error"
		job.Phase = "error"
		job.Error = stringPtr(err.Error())
		kind = "route_error"
		message = job.AdapterID + " → " + job.Address + ": " + err.Error()
	} else {
		job.State = "complete"
		job.Phase = "complete"
	}

	engine.assessRouteLocked(job)
	_ = storage.PublishJSON(job.LogFile, job)
	engine.mu.Unlock()
	engine.host.Event(kind, message)
}
