// os_keyring.go provides a KeyringProvider backed by the OS keyring
// (macOS Keychain, Windows Credential Manager, Linux Secret Service)
// via the go-keyring library.
package credentials

import (
	"github.com/zalando/go-keyring"
)

// OSKeyring implements KeyringProvider using the OS-native credential store.
type OSKeyring struct{}

// Get retrieves a password from the OS keyring.
func (o *OSKeyring) Get(service, account string) (string, error) {
	return keyring.Get(service, account)
}

// Set stores a password in the OS keyring.
func (o *OSKeyring) Set(service, account, password string) error {
	return keyring.Set(service, account, password)
}

// Delete removes a password from the OS keyring.
func (o *OSKeyring) Delete(service, account string) error {
	return keyring.Delete(service, account)
}

// NewManagerWithOSKeyring creates a Manager backed by the OS keyring.
// configAPIKey is the api_key value from config file (may be empty).
func NewManagerWithOSKeyring(configAPIKey string) *Manager {
	return NewManager(&OSKeyring{}, configAPIKey)
}
