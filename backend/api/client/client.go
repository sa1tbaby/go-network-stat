// Package client implements the local panel API's verified HTTP client.
package client

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const appID = "codex-internet-stability"

type Status struct {
	AppID         string `json:"appId"`
	ServerRunning bool   `json:"serverRunning"`
	Running       bool   `json:"running"`
	LogDirectory  string `json:"logDir"`
	IntervalMs    int    `json:"intervalMs"`
}

type Client struct {
	http *http.Client
}

func New() *Client {
	return &Client{http: &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{Proxy: nil, DisableKeepAlives: true}}}
}

func endpoint(base, path string) (string, error) {
	if !ValidBackendURL(base) {
		return "", errors.New("Invalid local server address")
	}

	return strings.TrimRight(base, "/") + "/" + path, nil
}

func (c *Client) Status(base string) (Status, error) {
	var state Status
	address, err := endpoint(base, "api/status")
	if err != nil {
		return state, err
	}

	response, err := c.http.Get(address)
	if err != nil {
		return state, err
	}

	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return state, fmt.Errorf("Server returned HTTP %d", response.StatusCode)
	}

	if err = json.NewDecoder(io.LimitReader(response.Body, 16<<20)).Decode(&state); err != nil {
		return state, err
	}

	if state.AppID != appID || !state.ServerRunning || state.LogDirectory == "" {
		return state, errors.New("Server did not confirm readiness")
	}

	return state, nil
}
