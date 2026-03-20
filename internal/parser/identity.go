package parser

import (
	"context"
	"os"
	"os/exec"
	"os/user"
	"strings"
	"sync"
	"time"
)

// Identity holds the four developer identity fields collected at
// startup.  Fields match src/identity.py collect_identity() exactly.
type Identity struct {
	GitName    *string // git config user.name
	GitEmail   *string // git config user.email
	OsUsername *string // OS login username
	MachineID  *string // hostname
}

// gitTimeout is the maximum duration for a git config subprocess.
const gitTimeout = 5 * time.Second

var (
	cachedIdentity *Identity
	identityOnce   sync.Once
)

// CollectIdentity gathers developer identity and caches the result.
// Subsequent calls return the cached value without re-running subprocesses.
// This mirrors the module-level caching in src/identity.py.
func CollectIdentity() *Identity {
	identityOnce.Do(func() {
		cachedIdentity = collectIdentityImpl()
	})
	return cachedIdentity
}

// ResetIdentityCache clears the cached identity, useful for testing.
func ResetIdentityCache() {
	cachedIdentity = nil
	identityOnce = sync.Once{}
}

// collectIdentityImpl does the actual collection work.
func collectIdentityImpl() *Identity {
	id := &Identity{}

	id.GitName = gitConfig("user.name")
	id.GitEmail = gitConfig("user.email")
	id.OsUsername = getOsUsername()
	id.MachineID = getMachineID()

	return id
}

// gitConfig runs "git config <key>" and returns the trimmed output,
// or nil on any failure.  Matches _git_config() in src/identity.py.
func gitConfig(key string) *string {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "config", key)
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	val := strings.TrimSpace(string(out))
	if val == "" {
		return nil
	}
	return &val
}

// getOsUsername returns the OS username.  It tries os.Getenv("USER")
// first (matching the Python fallback chain in _get_os_username()),
// then falls back to user.Current().
func getOsUsername() *string {
	if u := os.Getenv("USER"); u != "" {
		return &u
	}
	if u := os.Getenv("USERNAME"); u != "" {
		return &u
	}
	if cu, err := user.Current(); err == nil && cu.Username != "" {
		return &cu.Username
	}
	return nil
}

// getMachineID returns the machine hostname, matching platform.node()
// in src/identity.py.
func getMachineID() *string {
	h, err := os.Hostname()
	if err != nil || h == "" {
		return nil
	}
	return &h
}
