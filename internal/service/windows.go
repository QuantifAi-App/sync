//go:build windows

package service

import (
	"fmt"
	"os/exec"
)

const (
	// windowsServiceName is the Windows Service name.
	windowsServiceName = "QuantifaiSync"
	// legacyWindowsServiceName is the old service name used by ai-ops-shipper.
	legacyWindowsServiceName = "AIOpsShipper"
)

// windowsDisplayName is the user-facing display name shown in services.msc.
const windowsDisplayName = "Quantifai Sync Agent"

// Windows implements the Installer interface for Windows.  It registers
// a Windows Service using sc.exe with automatic start and restart-on-failure
// recovery settings (60-second delay between restart attempts).
type Windows struct{}

// ConfigPath returns an empty string because Windows Services are
// registered in the SCM registry, not as a config file on disk.
func (w *Windows) ConfigPath() string {
	return ""
}

// Install registers and starts the Windows Service with automatic start
// type and restart-on-failure recovery.
func (w *Windows) Install() error {
	// Create the service with automatic start
	out, err := exec.Command("sc", "create", windowsServiceName,
		fmt.Sprintf("binPath= %q", `C:\Program Files\quantifai\quantifai-sync.exe run`),
		fmt.Sprintf("DisplayName= %s", windowsDisplayName),
		"start= auto",
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("service: sc create: %s: %w", string(out), err)
	}

	// Configure recovery: restart on failure with 60-second delay
	out, err = exec.Command("sc", "failure", windowsServiceName,
		"reset= 86400",
		"actions= restart/60000/restart/60000/restart/60000",
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("service: sc failure: %s: %w", string(out), err)
	}

	// Start the service
	out, err = exec.Command("sc", "start", windowsServiceName).CombinedOutput()
	if err != nil {
		return fmt.Errorf("service: sc start: %s: %w", string(out), err)
	}

	return nil
}

// Uninstall stops and removes the Windows Service.
func (w *Windows) Uninstall() error {
	// Stop the service (best-effort)
	exec.Command("sc", "stop", windowsServiceName).CombinedOutput()

	// Delete the service
	out, err := exec.Command("sc", "delete", windowsServiceName).CombinedOutput()
	if err != nil {
		return fmt.Errorf("service: sc delete: %s: %w", string(out), err)
	}

	return nil
}

// MigrateFromOld detects the old AIOpsShipper Windows Service, stops it,
// and removes it.  Returns nil if no old service was found.
func (w *Windows) MigrateFromOld() error {
	// Check if old service exists (best-effort query)
	out, err := exec.Command("sc", "query", legacyWindowsServiceName).CombinedOutput()
	if err != nil {
		return nil // service not found, nothing to migrate
	}
	_ = out

	// Stop the old service (best-effort)
	exec.Command("sc", "stop", legacyWindowsServiceName).CombinedOutput()

	// Delete the old service
	if _, err := exec.Command("sc", "delete", legacyWindowsServiceName).CombinedOutput(); err != nil {
		return fmt.Errorf("service: sc delete legacy service: %w", err)
	}

	return nil
}

func init() {
	// Register the Windows installer in the factory so that
	// NewInstaller("windows") works when compiled on Windows.
	newWindowsInstaller = func() Installer {
		return &Windows{}
	}
}
