package networkmanager

import (
	"sort"
	"strconv"
	"time"
)

func iso() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

func sortedKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	return keys
}

func floatPtr(v float64) *float64 {
	return &v
}

func number(v float64) string {
	return strconv.FormatFloat(v, 'g', -1, 64)
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}

	return value
}

func storageStringPtr(v string) *string {
	return &v
}
