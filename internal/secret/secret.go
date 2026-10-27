// Package secret keeps the credentials of the hosts out of the configuration
// file.
//
// The values are handed to the keyring of the operating system when there is
// one: the credential manager of Windows, the keychain of macOS and the
// secret service of the desktop Linux distributions. A value the keyring
// cannot hold, and every value of a machine without a usable keyring, is
// kept in an encrypted file next to the configuration file.
package secret

import (
	"errors"
	"os"
	"strings"
	"sync"
)

// Service is the name the credentials are stored under in the keyring.
const Service = "ssh-tunnel"

// Field names a kind of credential.
type Field string

// The kinds of credential a host can have.
const (
	// FieldPassword is the password of the host.
	FieldPassword Field = "password"
	// FieldPassphrase decrypts the private key of the host.
	FieldPassphrase Field = "passphrase"
	// FieldKeyText is the private key of the host.
	FieldKeyText Field = "keytext"
)

// Fields lists every kind of credential.
var Fields = []Field{FieldPassword, FieldPassphrase, FieldKeyText}

var (
	// ErrNotFound is returned when an account holds no value.
	ErrNotFound = errors.New("the credential is not stored")
	// ErrTooLarge is returned when the keyring cannot hold a value.
	ErrTooLarge = errors.New("the credential is too large for the keyring")
)

// backend is a storage of credentials.
type backend interface {
	// Get returns the value of an account.
	Get(account string) (string, error)
	// Put stores the value of an account.
	Put(account string, value string) error
	// Delete removes an account.
	Delete(account string) error
}

// Store keeps the credentials of the hosts.
//
// The keyring of the system holds the values it can take; the others are
// kept in an encrypted file. A store is safe for concurrent use.
type Store struct {
	mu sync.Mutex
	// keyring holds the values of the keyring of the system. It is nil when
	// the machine has no usable keyring.
	keyring backend
	// file holds the values the keyring cannot take.
	file backend
	// dir is the directory the file backend works in.
	dir string
}

// Open returns the store of the machine. The directory is the one holding
// the configuration file: the fallback storage lives there.
//
// The environment variable SSH_TUNNEL_KEYRING, set to 0, off, false or no,
// keeps the credentials out of the keyring of the system.
func Open(dir string) (*Store, error) {
	return newStore(dir, keyringEnabled())
}

// keyringEnabled tells whether the keyring of the system may be used.
func keyringEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("SSH_TUNNEL_KEYRING"))) {
	case "0", "off", "false", "no":
		return false
	}
	return true
}

// OpenFile returns a store that keeps every credential in the encrypted
// file. It is used by the tests and by the callers that must not touch the
// keyring of the system.
func OpenFile(dir string) (*Store, error) {
	return newStore(dir, false)
}

func newStore(dir string, withKeyring bool) (*Store, error) {
	file, err := newFileBackend(dir)
	if err != nil {
		return nil, err
	}
	s := &Store{file: file, dir: dir}
	if withKeyring {
		if keyring := newKeyringBackend(); keyring.usable {
			s.keyring = keyring
		}
	}
	return s, nil
}

// Dir returns the directory the store works in.
func (s *Store) Dir() string { return s.dir }

// Backend describes where the credentials are kept.
func (s *Store) Backend() string {
	if s.keyring != nil {
		return "keyring"
	}
	return "encrypted file"
}

// Get returns the value of a credential of a host. The second result tells
// whether the credential is stored.
//
// The file wins over the keyring: a value is only written there when the
// keyring could not take it, so an old value of the keyring must not shadow
// it.
func (s *Store) Get(hostID string, f Field) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	account := accountName(hostID, f)
	value, fileErr := s.file.Get(account)
	if fileErr == nil {
		return value, true, nil
	}
	if s.keyring != nil {
		value, keyringErr := s.keyring.Get(account)
		if keyringErr == nil {
			return value, true, nil
		}
		if !errors.Is(keyringErr, ErrNotFound) && !errors.Is(fileErr, ErrNotFound) {
			return "", false, keyringErr
		}
	}
	if errors.Is(fileErr, ErrNotFound) {
		return "", false, nil
	}
	return "", false, fileErr
}

// Put stores the value of a credential of a host.
//
// A value that the keyring cannot take is written to the file, and the entry
// of the keyring is removed: the two places must not hold two values of the
// same credential.
func (s *Store) Put(hostID string, f Field, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	account := accountName(hostID, f)
	if s.keyring != nil {
		err := s.keyring.Put(account, value)
		if err == nil {
			// The keyring took the value: the copy in the file is not
			// needed any more.
			_ = s.file.Delete(account)
			return nil
		}
		// The keyring could not take the value: the file takes it. The old
		// entry of the keyring is removed afterwards, so that it does not
		// shadow the new value.
		if err := s.file.Put(account, value); err != nil {
			return err
		}
		_ = s.keyring.Delete(account)
		return nil
	}
	return s.file.Put(account, value)
}

// Delete removes a credential of a host. Removing a credential that is not
// stored is not an error.
func (s *Store) Delete(hostID string, f Field) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	account := accountName(hostID, f)
	err := s.file.Delete(account)
	if s.keyring != nil {
		keyringErr := s.keyring.Delete(account)
		if keyringErr != nil && !errors.Is(keyringErr, ErrNotFound) && err == nil {
			err = keyringErr
		}
	}
	return err
}

// DeleteHost removes every credential of a host.
func (s *Store) DeleteHost(hostID string) error {
	var firstErr error
	for _, f := range Fields {
		if err := s.Delete(hostID, f); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// accountName builds the name of the entry holding a credential of a host.
func accountName(hostID string, f Field) string {
	return hostID + ":" + string(f)
}
