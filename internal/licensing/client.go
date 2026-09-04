// Package licensing is the client side of the relay's licence system.
//
// It exists so the proxy never holds an upstream API key: instead it activates
// once against the relay using a licence key and a hardware id, caches the
// token it gets back, and attaches that token to every request from then on.
// The licence key itself is never needed again after the first successful run.
package licensing

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

var ErrNotLicensed = errors.New("licensing: this installation has not been activated")

// Client talks to the relay's activation endpoint.
type Client struct {
	BaseURL string
	HTTP    *http.Client
}

func NewClient(baseURL string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		HTTP:    &http.Client{Timeout: 15 * time.Second},
	}
}

// Activate exchanges a licence key and this machine's hardware id for a token.
// The exchange is permanent on the relay's side: the first machine to activate
// a key keeps it, so calling this again from the same machine is safe and
// calling it from a different machine fails.
func (c *Client) Activate(licenseKey, hwid string) (string, error) {
	body, err := json.Marshal(map[string]string{"key": licenseKey, "hwid": hwid})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequest(http.MethodPost, c.BaseURL+"/v1/activate", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("licensing: could not reach the licence server: %w", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("licensing: activation failed: %s", extractMessage(raw))
	}

	var out struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || out.Token == "" {
		return "", errors.New("licensing: activation response did not include a token")
	}
	return out.Token, nil
}

func extractMessage(raw []byte) string {
	var envelope struct {
		Message string `json:"message"`
		Error   struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &envelope); err == nil {
		if envelope.Error.Message != "" {
			return envelope.Error.Message
		}
		if envelope.Message != "" {
			return envelope.Message
		}
	}
	if len(raw) == 0 {
		return "no response body"
	}
	return string(raw)
}
