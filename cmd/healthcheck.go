package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// healthResponse matches the JSON schema returned by GET /healthz.
type healthResponse struct {
	Status         string `json:"status"`
	Version        string `json:"version"`
	UptimeSeconds  int    `json:"uptime_seconds"`
	LastSyncTime   string `json:"last_sync_time"`
	FilesTracked   int    `json:"files_tracked"`
	RecordsBufferd int    `json:"records_buffered"`
	ErrorsLastHour int    `json:"errors_last_hour"`
}

// RunHealthcheck hits the local /healthz endpoint and returns exit code
// 0 when the status is "ok", or 1 for any other status or connection error.
// The port parameter specifies the health server port (default 19876).
func RunHealthcheck(port int) int {
	return runHealthcheckWithClient(&http.Client{Timeout: 5 * time.Second}, port)
}

// runHealthcheckWithClient allows tests to inject a custom http.Client.
func runHealthcheckWithClient(client *http.Client, port int) int {
	url := fmt.Sprintf("http://127.0.0.1:%d/healthz", port)

	resp, err := client.Get(url)
	if err != nil {
		fmt.Printf("healthcheck failed: %v\n", err)
		return 1
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("healthcheck failed: could not read response: %v\n", err)
		return 1
	}

	if resp.StatusCode != http.StatusOK {
		fmt.Printf("healthcheck failed: HTTP %d\n", resp.StatusCode)
		return 1
	}

	var hr healthResponse
	if err := json.Unmarshal(body, &hr); err != nil {
		fmt.Printf("healthcheck failed: invalid JSON: %v\n", err)
		return 1
	}

	fmt.Printf("status: %s, version: %s, uptime: %ds\n", hr.Status, hr.Version, hr.UptimeSeconds)

	if hr.Status == "ok" {
		return 0
	}
	return 1
}
