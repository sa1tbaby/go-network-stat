package networkmanager

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sa1tbaby/go-network-stat/backend/networkmanager/collectors"
	"github.com/sa1tbaby/go-network-stat/backend/networkmanager/routes"
	"github.com/sa1tbaby/go-network-stat/backend/networkmanager/storage"
)

type loadedStore struct {
	targets  map[string]*target
	adapters map[string]collectors.Adapter
	events   []EventRow
	routes   map[string]*routes.Job
	started  string
	skipped  int
}

func emptyStore() *loadedStore {
	return &loadedStore{
		targets:  map[string]*target{},
		adapters: map[string]collectors.Adapter{},
		routes:   map[string]*routes.Job{},
		events:   []EventRow{},
	}
}

func readStore(dir string) (*loadedStore, error) {
	data := emptyStore()
	kinds := map[string]string{}
	statusPath := filepath.Join(dir, "status.json")
	if b, err := os.ReadFile(statusPath); err == nil && len(b) > 0 {
		var status struct {
			Started  string               `json:"started"`
			Adapters []collectors.Adapter `json:"adapters"`
			Targets  []targetSnapshot     `json:"targets"`
		}
		if err = json.Unmarshal(storage.WithoutBOM(b), &status); err != nil {
			return nil, err
		}

		data.started = status.Started
		for _, a := range status.Adapters {
			if a.ID != "" {
				a.Available = false
				a.State = "Historical"
				data.adapters[a.ID] = a
			}
		}

		for _, t := range status.Targets {
			kinds[targetID(t.AdapterID, t.Protocol, t.Address, t.Port)] = t.Kind
		}
	} else if err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	if f, err := os.Open(filepath.Join(dir, "probes.csv")); err == nil {
		reader := bufio.NewScanner(f)
		reader.Buffer(make([]byte, 4096), 1024*1024)
		reader.Scan()
		for reader.Scan() {
			line := strings.TrimSpace(reader.Text())
			if line == "" {
				continue
			}

			c, parseErr := csv.NewReader(strings.NewReader(line)).Read()
			if parseErr != nil || len(c) != 12 {
				data.skipped++
				continue
			}

			tick, e1 := strconv.ParseInt(c[1], 10, 64)
			port, e2 := strconv.Atoi(c[7])
			ok, e3 := strconv.ParseBool(c[8])
			tool, e4 := strconv.ParseBool(c[11])
			address := net.ParseIP(c[6])
			var rtt *float64
			if ok {
				r, e := strconv.ParseFloat(c[10], 64)
				if e != nil || math.IsNaN(r) || math.IsInf(r, 0) || r < 0 {
					data.skipped++
					continue
				}

				rtt = &r
			}

			if e1 != nil || e2 != nil || e3 != nil || e4 != nil || c[2] == "" || (c[5] != "icmp" && c[5] != "tcp") || address == nil || address.To4() == nil || port < 0 || port > 65535 {
				data.skipped++
				continue
			}

			id := targetID(c[2], c[5], c[6], port)
			t := data.targets[id]
			if t == nil {
				kind := kinds[id]
				if kind != "gateway" && kind != "internet" && kind != "custom" {
					kind = "custom"
					if c[6] == "1.1.1.1" || c[6] == "8.8.8.8" {
						kind = "internet"
					}
				}

				t = &target{
					ID:             id,
					AdapterID:      c[2],
					AdapterName:    c[2],
					Address:        c[6],
					Protocol:       c[5],
					Port:           port,
					Kind:           kind,
					HistoricalOnly: true,
					Recent:         []Point{},
				}
				data.targets[id] = t
			}

			restorePoint(t, Point{
				T:         tick,
				OK:        ok,
				ToolError: tool,
				Status:    c[9],
				RTTMs:     rtt,
			}, time.Now().UnixMilli())
			if data.started == "" {
				data.started = c[0]
			}

			if _, exists := data.adapters[c[2]]; !exists {
				index, _ := strconv.Atoi(c[4])
				data.adapters[c[2]] = collectors.Adapter{
					ID:       c[2],
					Name:     c[2],
					Kind:     "historical",
					State:    "Historical",
					IPv4:     c[3],
					Index:    index,
					ICMPSafe: true,
				}
			}
		}

		err = reader.Err()
		f.Close()
		if err != nil {
			return nil, err
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}

	if f, err := os.Open(filepath.Join(dir, "events.jsonl")); err == nil {
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 4096), 1024*1024)
		for scanner.Scan() {
			if len(scanner.Bytes()) == 0 {
				continue
			}

			var event EventRow
			if json.Unmarshal(storage.WithoutBOM(scanner.Bytes()), &event) != nil || event.Time == "" || event.Kind == "" {
				data.skipped++
				continue
			}

			data.events = append(data.events, event)
			if len(data.events) > 100 {
				data.events = data.events[1:]
			}
		}

		err = scanner.Err()
		f.Close()
		if err != nil {
			return nil, err
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}

	files, _ := filepath.Glob(filepath.Join(dir, "routes", "*.json"))
	sort.Strings(files)
	for _, path := range files {
		if storage.NoLink(path) != nil {
			continue
		}

		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		var route routes.Job
		if json.Unmarshal(storage.WithoutBOM(b), &route) != nil || route.AdapterID == "" || route.Address == "" || !storage.SamePath(route.LogFile, path) {
			continue
		}

		if route.State == "running" {
			route.State = "error"
			route.Error = storageStringPtr("An unfinished route test from the previous session was recovered.")
		}

		data.routes[route.AdapterID+"|"+route.Address] = &route
	}

	return data, nil
}

func (m *Manager) applyStoreLocked(data *loadedStore) {
	m.targets = data.targets
	for id, a := range m.adapters {
		if a.State == "Historical" {
			delete(m.adapters, id)
		}
	}

	for id, a := range data.adapters {
		if _, exists := m.adapters[id]; !exists {
			m.adapters[id] = a
		}
	}

	m.events = data.events
	m.routeEngine.Restore(data.routes)
	m.started = data.started
	if m.started == "" {
		m.started = iso()
	}

	m.storageWarning = ""
	if data.skipped > 0 {
		m.storageWarning = fmt.Sprintf("Skipped incomplete or damaged records: %d", data.skipped)
	}

	for _, a := range m.adapters {
		m.ensureAdapterTargetsLocked(a)
	}
}
