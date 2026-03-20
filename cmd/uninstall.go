package cmd

import (
	"fmt"
	"runtime"

	"github.com/quantifai/sync/internal/service"
)

// RunUninstall detects the current platform, stops the running service,
// removes the service configuration file, and unregisters the service.
func RunUninstall() int {
	platform := runtime.GOOS

	installer, err := service.NewInstaller(platform)
	if err != nil {
		fmt.Printf("uninstall failed: %v\n", err)
		return 1
	}

	if err := installer.Uninstall(); err != nil {
		fmt.Printf("uninstall failed: %v\n", err)
		return 1
	}

	fmt.Printf("quantifai-sync uninstalled successfully on %s\n", platform)
	return 0
}
