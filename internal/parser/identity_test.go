package parser

import (
	"testing"
)

// TestIdentityCollectionReturnsAllFields verifies that CollectIdentity
// returns an Identity struct with all 4 fields populated (at least
// os_username and machine_id should be non-nil on any dev machine).
// This mirrors the contract of src/identity.py collect_identity().
func TestIdentityCollectionReturnsAllFields(t *testing.T) {
	ResetIdentityCache()
	defer ResetIdentityCache()

	id := CollectIdentity()
	if id == nil {
		t.Fatal("CollectIdentity returned nil")
	}

	// os_username should always be available on a dev machine
	if id.OsUsername == nil {
		t.Error("OsUsername is nil; expected a value from USER env or user.Current()")
	}

	// machine_id (hostname) should always be available
	if id.MachineID == nil {
		t.Error("MachineID is nil; expected a hostname from os.Hostname()")
	}

	// git_name and git_email depend on git being configured; we only
	// check that the fields exist (non-nil) or are explicitly nil without
	// panicking.  On CI where git is configured, they will be non-nil.
	// The important thing is that the struct is valid.
	t.Logf("GitName:    %v", ptrStr(id.GitName))
	t.Logf("GitEmail:   %v", ptrStr(id.GitEmail))
	t.Logf("OsUsername: %v", ptrStr(id.OsUsername))
	t.Logf("MachineID:  %v", ptrStr(id.MachineID))
}

// TestIdentityCaching verifies that CollectIdentity returns the same
// cached instance on subsequent calls.
func TestIdentityCaching(t *testing.T) {
	ResetIdentityCache()
	defer ResetIdentityCache()

	first := CollectIdentity()
	second := CollectIdentity()

	if first != second {
		t.Error("expected CollectIdentity to return the same cached pointer")
	}
}

// ptrStr dereferences a *string for test logging.
func ptrStr(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}
