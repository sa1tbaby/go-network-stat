package storage

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/sa1tbaby/go-network-stat/backend/infrastructure/fileutil"
)

const AppID = "codex-internet-stability"

type eventIdentity struct {
	Time    string
	Kind    string
	Message string
}

const ProbeHeader = "time,unix_ms,adapter,source_ip,interface_index,protocol,target,port,ok,status,rtt_ms,tool_error"

const AdapterHeader = "time,elapsed_ms,adapter,index,state,ipv4,gateway,speed_mbps,rx_errors,tx_errors,rx_discards,tx_discards"

func FullPath(path string) (string, error) {
	if strings.TrimSpace(path) == "" || !filepath.IsAbs(path) {
		return "", errors.New("Enter an absolute path to the log folder.")
	}

	if strings.ContainsAny(path, "\r\n\x00") || strings.HasPrefix(path, `\\.\`) || strings.HasPrefix(path, `\\?\`) {
		return "", errors.New("Invalid folder path.")
	}

	return filepath.Clean(path), nil
}

func SamePath(a, b string) bool {
	aa, e := filepath.Abs(a)
	if e != nil {
		return false
	}

	bb, e := filepath.Abs(b)
	if e != nil {
		return false
	}

	return strings.EqualFold(filepath.Clean(aa), filepath.Clean(bb))
}

func NoLink(path string) error {
	for {
		info, err := os.Lstat(path)
		if err != nil && !os.IsNotExist(err) {
			return err
		}

		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("The log path is a link: %s", path)
		}

		parent := filepath.Dir(path)
		if parent == path || parent == "." {
			return nil
		}

		path = parent
	}
}

func JSONHas(path, key, value string) bool {
	var data map[string]any
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}

	return json.Unmarshal(WithoutBOM(b), &data) == nil && data[key] == value
}

func WithoutBOM(data []byte) []byte {
	if len(data) >= 3 && data[0] == 0xef && data[1] == 0xbb && data[2] == 0xbf {
		return data[3:]
	}

	return data
}

func PublishJSON(path string, data any) error {
	if err := NoLink(path); err != nil {
		return err
	}

	b, err := json.Marshal(data)
	if err != nil {
		return err
	}

	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+"-*.tmp")
	if err != nil {
		return err
	}

	temp := f.Name()
	defer os.Remove(temp)
	if _, err = f.Write(b); err != nil {
		f.Close()

		return err
	}

	if err = f.Sync(); err != nil {
		f.Close()

		return err
	}

	if err = f.Close(); err != nil {
		return err
	}

	return fileutil.Replace(temp, path)
}

func Validate(dir string) error {
	if err := NoLink(dir); err != nil {
		return err
	}

	for _, pair := range [][2]string{{"probes.csv", ProbeHeader}, {"adapters.csv", AdapterHeader}} {
		path := filepath.Join(dir, pair[0])
		if err := NoLink(path); err != nil {
			return err
		}

		f, err := os.Open(path)
		if os.IsNotExist(err) {
			continue
		}

		if err != nil {
			return err
		}

		r := bufio.NewReader(f)
		header, readErr := r.ReadString('\n')
		f.Close()
		if header != "" && strings.Trim(strings.TrimPrefix(header, "\ufeff"), "\r\n") != pair[1] {
			return fmt.Errorf("The folder contains an unrelated file %s. Choose another folder.", pair[0])
		}

		if readErr != nil && readErr != io.EOF {
			return readErr
		}
	}

	for _, item := range [][3]string{
		{"log-store.json", "appId", AppID},
		{"status.json", "appId", AppID},
		{"diagnostics-live.json", "appId", "codex-single-pc-diagnostics"},
		{"radio-live.json", "kind", "wifi_radio"},
		{"ethernet-live.json", "kind", "ethernet_line"},
		{"diagnosis-index.json", "appId", "codex-network-diagnosis-index"},
	} {
		path := filepath.Join(dir, item[0])
		if err := NoLink(path); err != nil {
			return err
		}

		if stat, err := os.Stat(path); err == nil && stat.Size() > 0 && !JSONHas(path, item[1], item[2]) {
			return fmt.Errorf("File %s is not a log from this dashboard.", item[0])
		} else if err != nil && !os.IsNotExist(err) {
			return err
		}
	}

	path := filepath.Join(dir, "events.jsonl")
	if err := NoLink(path); err != nil {
		return err
	}

	if f, err := os.Open(path); err == nil {
		scanner := bufio.NewScanner(f)
		if scanner.Scan() {
			var e eventIdentity
			err = json.Unmarshal(WithoutBOM(scanner.Bytes()), &e)
			if err != nil || e.Time == "" || e.Kind == "" {
				f.Close()

				return errors.New("events.jsonl has an unknown format.")
			}
		}

		scanErr := scanner.Err()
		f.Close()
		if scanErr != nil {
			return scanErr
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	for _, sub := range []string{"routes", "diagnostics", "radio", "ethernet", "diagnosis-runs", "diagnostics-stop.flag"} {
		if err := NoLink(filepath.Join(dir, sub)); err != nil {
			return err
		}
	}

	return nil
}
