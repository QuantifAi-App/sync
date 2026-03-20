package cmd

import (
	"github.com/quantifai/sync/internal/config"
	"github.com/quantifai/sync/internal/tray"
)

// RunTray starts the macOS menu bar tray icon. It reads the health
// server port from config and polls the local /healthz endpoint to
// display pipeline status in the system tray.
//
// The tray process is separate from the LaunchAgent daemon — it runs
// in the user's GUI session. On non-macOS platforms, it prints an
// error message and returns 1.
func RunTray() int {
	cfg, _ := config.Load("", "")
	dashboardURL := "https://quantifai.app"
	tray.Run(cfg.HealthPort, dashboardURL)
	return 0
}
