// Package targets owns the persistent user target registry, independent of probes and HTTP.
package targets

import (
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/sa1tbaby/go-network-stat/backend/networkmanager/storage"
)

func nullableString(s string) any {
	if s == "" {
		return nil
	}

	return s
}

type Target struct {
	Address      string `json:"address"`
	Host         string `json:"host"`
	Port         int    `json:"port"`
	AddedAt      string `json:"addedAt"`
	Game         string `json:"game,omitempty"`
	Label        string `json:"label,omitempty"`
	Origin       string `json:"origin,omitempty"`
	ObservedAt   string `json:"observedAt,omitempty"`
	SourceIP     string `json:"sourceIp,omitempty"`
	EvidenceFile string `json:"evidenceFile,omitempty"`
	UDPPort      int    `json:"udpPort"`
	LocalPort    int    `json:"localPort"`
	InEvents     int    `json:"inEvents"`
	OutEvents    int    `json:"outEvents"`
}

func (t Target) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{
		"address":      t.Address,
		"host":         t.Host,
		"port":         t.Port,
		"addedAt":      t.AddedAt,
		"game":         nullableString(t.Game),
		"label":        nullableString(t.Label),
		"origin":       nullableString(t.Origin),
		"observedAt":   nullableString(t.ObservedAt),
		"sourceIp":     nullableString(t.SourceIP),
		"evidenceFile": nullableString(t.EvidenceFile),
		"udpPort":      t.UDPPort,
		"localPort":    t.LocalPort,
		"inEvents":     t.InEvents,
		"outEvents":    t.OutEvents,
	})
}

type Registry struct {
	mu    sync.Mutex
	path  string
	items map[string]Target
}

func Open(stateDirectory string) (*Registry, error) {
	path := filepath.Join(stateDirectory, "custom-targets.json")
	if err := storage.NoLink(path); err != nil {
		return nil, err
	}

	r := &Registry{path: path, items: map[string]Target{}}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return r, nil
	}

	if err != nil {
		return nil, err
	}

	var values []Target
	if json.Unmarshal(storage.WithoutBOM(data), &values) != nil {
		return r, nil
	}

	for _, v := range values {
		if ip := net.ParseIP(v.Address); ip != nil && ip.To4() != nil && len(r.items) < 8 {
			r.items[v.Address] = v
		}
	}

	return r, nil
}

func (r *Registry) Get(address string) (Target, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.items[address]

	return v, ok
}

func (r *Registry) All() []Target {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.allLocked()
}

func (r *Registry) allLocked() []Target {
	keys := make([]string, 0, len(r.items))
	for k := range r.items {
		keys = append(keys, k)
	}

	sort.Strings(keys)
	out := make([]Target, 0, len(keys))
	for _, k := range keys {
		out = append(out, r.items[k])
	}

	return out
}

func (r *Registry) Put(value Target) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	old, exists := r.items[value.Address]
	if !exists && len(r.items) >= 8 {
		return errors.New("Up to 8 custom addresses are allowed at a time.")
	}

	r.items[value.Address] = value
	if err := storage.PublishJSON(r.path, r.allLocked()); err != nil {
		if exists {
			r.items[value.Address] = old
		} else {
			delete(r.items, value.Address)
		}

		return err
	}

	return nil
}

func (r *Registry) Delete(address string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	old, exists := r.items[address]
	if !exists {
		return errors.New("Custom target not found.")
	}

	delete(r.items, address)
	if err := storage.PublishJSON(r.path, r.allLocked()); err != nil {
		r.items[address] = old

		return err
	}

	return nil
}
