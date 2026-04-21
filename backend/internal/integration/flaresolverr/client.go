package flaresolverr

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// TestConnection verifies that a FlareSolverr instance at baseURL is reachable
// and responding. Returns (ok, message, error).
func TestConnection(baseURL string, httpClient *http.Client) (bool, string, error) {
	payload, _ := json.Marshal(map[string]any{
		"cmd":        "request.get",
		"url":        "http://www.google.com",
		"maxTimeout": 15000,
	})

	resp, err := httpClient.Post(
		strings.TrimRight(baseURL, "/")+"/v1",
		"application/json",
		bytes.NewReader(payload),
	)
	if err != nil {
		return false, fmt.Sprintf("Connection failed: %v", err), nil
	}
	defer resp.Body.Close()

	var result struct {
		Status  string `json:"status"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return false, fmt.Sprintf("Invalid response: %v", err), nil
	}
	if result.Status != "ok" {
		return false, fmt.Sprintf("FlareSolverr error: %s", result.Message), nil
	}
	return true, "Connection successful", nil
}
