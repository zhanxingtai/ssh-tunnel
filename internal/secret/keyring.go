package secret

import (
	"crypto/rand"
	"encoding/hex"
	"errors"

	"github.com/zalando/go-keyring"
)

// keyringBackend keeps the credentials in the keyring of the system.
type keyringBackend struct {
	// usable tells whether the keyring answers at all.
	usable bool
}

// newKeyringBackend returns the keyring of the system when it answers. A
// machine without a secret service, for instance a headless server, has no
// usable keyring.
func newKeyringBackend() *keyringBackend {
	b := &keyringBackend{}
	_, err := keyring.Get(Service, probeAccount())
	if err == nil || errors.Is(err, keyring.ErrNotFound) {
		b.usable = true
	}
	return b
}

// probeAccount returns an account name that no host can use.
func probeAccount() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "probe"
	}
	return "probe-" + hex.EncodeToString(b[:])
}

// Get returns the value of an account.
func (b *keyringBackend) Get(account string) (string, error) {
	value, err := keyring.Get(Service, account)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	return value, nil
}

// Put stores the value of an account.
func (b *keyringBackend) Put(account string, value string) error {
	err := keyring.Set(Service, account, value)
	if errors.Is(err, keyring.ErrSetDataTooBig) {
		return ErrTooLarge
	}
	return err
}

// Delete removes an account.
func (b *keyringBackend) Delete(account string) error {
	err := keyring.Delete(Service, account)
	if errors.Is(err, keyring.ErrNotFound) {
		return ErrNotFound
	}
	return err
}
