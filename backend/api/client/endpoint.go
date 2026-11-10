package client

import (
	"net/url"
	"strconv"
)

// ValidBackendURL validates endpoints received from an owned backend process.
func ValidBackendURL(address string) bool {
	addressURL, err := url.Parse(address)
	if err != nil || addressURL.Scheme != "http" || addressURL.Hostname() != "127.0.0.1" || addressURL.User != nil || addressURL.RawQuery != "" || addressURL.Fragment != "" || (addressURL.Path != "" && addressURL.Path != "/") {
		return false
	}

	port, err := strconv.Atoi(addressURL.Port())

	return err == nil && port > 0 && port <= 65535
}
