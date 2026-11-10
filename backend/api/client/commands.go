// Package client implements the local panel API's verified HTTP client.
package client

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

func (c *Client) SelectLogDirectory(base, path string) error {
	address, err := endpoint(base, "api/logs/select?path="+url.QueryEscape(path))
	if err != nil {
		return err
	}

	response, err := c.http.Post(address, "application/json", nil)
	if err != nil {
		return err
	}

	defer response.Body.Close()
	var result struct {
		OK        bool   `json:"ok"`
		Error     string `json:"error"`
		Directory string `json:"directory"`
	}
	if err = json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&result); err != nil {
		return err
	}

	if response.StatusCode != http.StatusOK || !result.OK {
		if result.Error != "" {
			return errors.New(result.Error)
		}

		return fmt.Errorf("Failed to change the folder (HTTP %d)", response.StatusCode)
	}

	return nil
}

func (c *Client) command(base, path string) error {
	address, err := endpoint(base, path)
	if err != nil {
		return err
	}

	response, err := c.http.Post(address, "application/json", nil)
	if err != nil {
		return err
	}

	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", response.StatusCode)
	}

	return nil
}

func (c *Client) Shutdown(base string) error {
	return c.command(base, "api/shutdown")
}

func (c *Client) Pause(base string) error {
	if _, err := c.Status(base); err != nil {
		return err
	}

	if err := c.command(base, "api/stop"); err != nil {
		return fmt.Errorf("pause: %w", err)
	}

	return nil
}
