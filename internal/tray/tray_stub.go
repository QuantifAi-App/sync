//go:build !darwin

package tray

import "fmt"

// Run prints an error and returns because the tray icon is only
// supported on macOS. On other platforms, use the healthcheck command
// to monitor the agent status.
func Run(healthPort int, dashboardURL string) {
	fmt.Println("error: tray icon is only supported on macOS")
	fmt.Println("use 'quantifai-sync healthcheck' to check agent status")
}
