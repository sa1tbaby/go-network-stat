package networkmanager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/sa1tbaby/go-network-stat/backend/contracts"
	"github.com/sa1tbaby/go-network-stat/backend/networkmanager/collectors"
	"github.com/sa1tbaby/go-network-stat/backend/networkmanager/targets"
)

func (m *Manager) AddTarget(host, port string) (any, error) {
	return m.command(func() (any, error) {
		return m.addCustom(host, port)
	})
}

func (m *Manager) RemoveTarget(address string) (any, error) {
	return m.command(func() (any, error) {
		return m.removeCustom(address)
	})
}

func (m *Manager) AddControlTarget(address string) (any, error) {
	return m.command(func() (any, error) {
		return m.addControl(address)
	})
}

type CustomTarget = targets.Target

var hostPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

func validRoutableIPv4(address string) bool {
	ip := net.ParseIP(address).To4()

	return ip != nil && ip[0] != 0 && ip[0] < 224 && !(ip[0] == 169 && ip[1] == 254)
}

func (m *Manager) addCustom(host, portText string) (any, error) {
	host = strings.TrimSpace(host)
	portText = strings.TrimSpace(portText)
	if host == "" || len(host) > 253 || !hostPattern.MatchString(host) {
		return nil, errors.New("Enter an IPv4 address or hostname without a protocol or port.")
	}

	port := 0
	if portText != "" {
		var err error
		port, err = strconv.Atoi(portText)
		if err != nil || port < 1 || port > 65535 {
			return nil, errors.New("The TCP port must be between 1 and 65535; leave it empty for ICMP.")
		}
	}

	ip := net.ParseIP(host)
	if ip == nil {
		ctx, cancel := context.WithTimeout(m.ctx, 3*time.Second)
		defer cancel()
		addresses, err := net.DefaultResolver.LookupIP(ctx, "ip4", host)
		if err != nil {
			return nil, fmt.Errorf("DNS: %w", err)
		}

		for _, a := range addresses {
			if a.To4() != nil {
				ip = a
				break
			}
		}
	}

	if ip == nil || ip.To4() == nil {
		return nil, errors.New("This test requires an IPv4 address.")
	}

	address := ip.To4().String()
	if !validRoutableIPv4(address) {
		return nil, errors.New("This address cannot be used for a routed test. Steam FakeIP cannot be pinged.")
	}

	if address == "1.1.1.1" || address == "8.8.8.8" {
		return nil, errors.New("This address is already used by the built-in checks.")
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.custom.Get(address); !exists && len(m.custom.All()) >= 8 {
		return nil, errors.New("Up to 8 custom addresses are allowed at a time.")
	}

	for _, a := range m.adapters {
		if a.Gateway == address {
			return nil, errors.New("This address is already monitored as a gateway.")
		}
	}

	if err := m.custom.Put(CustomTarget{
		Address: address,
		Host:    host,
		Port:    port,
		AddedAt: iso(),
	}); err != nil {
		return nil, err
	}

	for id, t := range m.targets {
		if t.Kind == "custom" && t.Address == address {
			delete(m.targets, id)
		}
	}

	for _, a := range m.adapters {
		m.ensureAdapterTargetsLocked(a)
	}

	m.eventLocked("target_added", fmt.Sprintf("Added target %s (%s)", host, address))

	return map[string]any{
		"ok":      true,
		"address": address,
		"port":    port,
	}, nil
}

func (m *Manager) removeCustom(address string) (any, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, exists := m.custom.Get(address)
	if !exists {
		return nil, errors.New("Custom target not found.")
	}

	if err := m.custom.Delete(address); err != nil {
		return nil, err
	}

	for id, target := range m.targets {
		if target.Kind == "custom" && target.Address == address {
			delete(m.targets, id)
		}
	}

	m.eventLocked("target_removed", "Removed target "+address)

	return map[string]any{"ok": true}, nil
}

func (m *Manager) AddDetectedTarget(game string, peer contracts.DetectedTarget, evidenceFile string) error {
	if !collectors.PublicIPv4(peer.Address) || peer.UDPPort < 1 || peer.UDPPort > 65535 {
		return errors.New("The capture did not confirm a usable IPv4 UDP peer.")
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	existing, exists := m.custom.Get(peer.Address)
	if !exists && len(m.custom.All()) >= 8 {
		return errors.New("Up to 8 custom addresses are allowed at a time.")
	}

	observed := peer.ObservedAt
	if observed == "" {
		observed = iso()
	}

	label := peer.Label
	if label == "" {
		label = strings.ToUpper(game) + " · observed UDP peer"
	}

	custom := existing
	if !exists {
		custom = CustomTarget{
			Address: peer.Address,
			Host:    peer.Address,
			AddedAt: iso(),
		}
	}

	custom.Game = game
	custom.Label = label
	custom.Origin = game + "_etw"
	custom.ObservedAt = observed
	custom.SourceIP = peer.SourceIP
	custom.EvidenceFile = evidenceFile
	custom.UDPPort = peer.UDPPort
	custom.LocalPort = peer.LocalPort
	custom.InEvents = peer.InEvents
	custom.OutEvents = peer.OutEvents
	if err := m.custom.Put(custom); err != nil {
		return err
	}

	for _, a := range m.adapters {
		m.ensureAdapterTargetsLocked(a)
	}

	m.eventLocked(game+"_relay_added", strings.ToUpper(game)+": observed UDP peer "+peer.Address)

	return nil
}

func (m *Manager) addControl(address string) (any, error) {
	var catalog struct {
		Regions []struct {
			Targets []struct {
				Address string
				Label   string
			}
		}
	}
	b, err := os.ReadFile(filepath.Join(m.cfg.Root, "cod-test-targets.json"))
	if err != nil {
		return nil, err
	}

	if err = json.Unmarshal(b, &catalog); err != nil {
		return nil, err
	}

	label := ""
	for _, region := range catalog.Regions {
		for _, target := range region.Targets {
			if target.Address == address {
				label = target.Label
			}
		}
	}

	if label == "" {
		return nil, errors.New("Unknown CoD checkpoint.")
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	for _, a := range m.adapters {
		if a.Gateway == address {
			return map[string]any{
				"ok":       true,
				"address":  address,
				"existing": true,
				"message":  "This address is already monitored in the router card.",
			}, nil
		}
	}

	custom, exists := m.custom.Get(address)
	if !exists {
		if len(m.custom.All()) >= 8 {
			return nil, errors.New("Up to 8 custom addresses are allowed at a time.")
		}

		custom = CustomTarget{
			Address: address,
			Host:    address,
			AddedAt: iso(),
		}
	}

	if custom.Origin != "cod_etw" {
		custom.Game = "cod"
		custom.Label = label
		custom.Origin = "control_catalog"
		if err = m.custom.Put(custom); err != nil {
			return nil, err
		}
	}

	for _, a := range m.adapters {
		m.ensureAdapterTargetsLocked(a)
	}

	return map[string]any{
		"ok":       true,
		"address":  address,
		"existing": false,
	}, nil
}
