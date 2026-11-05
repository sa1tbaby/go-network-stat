package storage

import (
	"encoding/csv"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"time"
)

type Store struct {
	probes   *os.File
	adapters *os.File
	events   *os.File
	opened   time.Time
}

func (w *Store) Close() error {
	var result error
	for _, f := range []*os.File{w.probes, w.adapters, w.events} {
		if f != nil {
			if e := f.Close(); result == nil {
				result = e
			}
		}
	}

	return result
}

func appendLog(path, header string) (*os.File, error) {
	if err := NoLink(path); err != nil {
		return nil, err
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0600)
	if err != nil {
		return nil, err
	}

	stat, err := f.Stat()
	if err != nil {
		f.Close()

		return nil, err
	}

	if stat.Size() == 0 && header != "" {
		if _, err = f.WriteString(header + "\n"); err != nil {
			f.Close()

			return nil, err
		}
	}

	return f, nil
}

func Open(dir string) (*Store, error) {
	if err := Validate(dir); err != nil {
		return nil, err
	}

	w := &Store{opened: time.Now()}
	var err error
	w.probes, err = appendLog(filepath.Join(dir, "probes.csv"), ProbeHeader)
	if err == nil {
		w.adapters, err = appendLog(filepath.Join(dir, "adapters.csv"), AdapterHeader)
	}

	if err == nil {
		w.events, err = appendLog(filepath.Join(dir, "events.jsonl"), "")
	}

	if err != nil {
		w.Close()

		return nil, err
	}

	return w, nil
}

// Store owns open append-only monitor logs. Callers serialize writes and Reset.
func (w *Store) AppendProbe(row []string) error {
	return appendCSV(w.probes, row)
}

func (w *Store) AppendAdapter(row []string) error {
	return appendCSV(w.adapters, row)
}

func (w *Store) AppendEvent(event any) error {
	return json.NewEncoder(w.events).Encode(event)
}

func (w *Store) Elapsed() time.Duration {
	return time.Since(w.opened)
}

func appendCSV(file *os.File, row []string) error {
	writer := csv.NewWriter(file)
	if err := writer.Write(row); err != nil {
		return err
	}

	writer.Flush()

	return writer.Error()
}

func (w *Store) Reset() error {
	for _, item := range []struct {
		file   *os.File
		header string
	}{{w.probes, ProbeHeader}, {w.adapters, AdapterHeader}, {w.events, ""}} {
		if err := item.file.Truncate(0); err != nil {
			return err
		}

		if _, err := item.file.Seek(0, io.SeekStart); err != nil {
			return err
		}

		if item.header != "" {
			if _, err := item.file.WriteString(item.header + "\n"); err != nil {
				return err
			}
		}
	}

	return nil
}
