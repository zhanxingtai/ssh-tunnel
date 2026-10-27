package secret

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testStore returns a store that keeps the credentials in the encrypted file
// of a temporary directory.
func testStore(t *testing.T) *Store {
	t.Helper()
	store, err := OpenFile(t.TempDir())
	if err != nil {
		t.Fatalf("open the store: %v", err)
	}
	return store
}

func TestStoreKeepsTheCredentials(t *testing.T) {
	store := testStore(t)
	if _, ok, err := store.Get("h1", FieldPassword); err != nil || ok {
		t.Fatalf("a missing credential was found: %v %v", ok, err)
	}

	if err := store.Put("h1", FieldPassword, "s3cret"); err != nil {
		t.Fatalf("put: %v", err)
	}
	value, ok, err := store.Get("h1", FieldPassword)
	if err != nil || !ok {
		t.Fatalf("the credential was not found: %v %v", ok, err)
	}
	if value != "s3cret" {
		t.Fatalf("unexpected credential %q", value)
	}

	if err := store.Delete("h1", FieldPassword); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, ok, err := store.Get("h1", FieldPassword); err != nil || ok {
		t.Fatalf("the credential was not removed: %v %v", ok, err)
	}
	// Removing a credential that is not there is not an error.
	if err := store.Delete("h1", FieldPassword); err != nil {
		t.Fatalf("delete: %v", err)
	}
}

func TestStoreKeepsTheCredentialsOfEveryHostApart(t *testing.T) {
	store := testStore(t)
	if err := store.Put("h1", FieldPassword, "one"); err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := store.Put("h2", FieldPassword, "two"); err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := store.Put("h1", FieldPassphrase, "phrase"); err != nil {
		t.Fatalf("put: %v", err)
	}

	value, _, _ := store.Get("h1", FieldPassword)
	if value != "one" {
		t.Errorf("unexpected credential %q", value)
	}
	value, _, _ = store.Get("h2", FieldPassword)
	if value != "two" {
		t.Errorf("unexpected credential %q", value)
	}
	value, _, _ = store.Get("h1", FieldPassphrase)
	if value != "phrase" {
		t.Errorf("unexpected credential %q", value)
	}

	if err := store.DeleteHost("h1"); err != nil {
		t.Fatalf("delete the host: %v", err)
	}
	if _, ok, _ := store.Get("h1", FieldPassword); ok {
		t.Error("the password was not removed")
	}
	if _, ok, _ := store.Get("h1", FieldPassphrase); ok {
		t.Error("the passphrase was not removed")
	}
	if _, ok, _ := store.Get("h2", FieldPassword); !ok {
		t.Error("the credentials of the other host were removed")
	}
}

// TestTheCredentialsAreNotInClearText makes sure that the file of the store
// does not hold the credentials in clear text.
func TestTheCredentialsAreNotInClearText(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenFile(dir)
	if err != nil {
		t.Fatalf("open the store: %v", err)
	}
	if err := store.Put("h1", FieldKeyText, "-----BEGIN OPENSSH PRIVATE KEY-----"); err != nil {
		t.Fatalf("put: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, dataFileName))
	if err != nil {
		t.Fatalf("read the store: %v", err)
	}
	if strings.Contains(string(data), "BEGIN OPENSSH PRIVATE KEY") {
		t.Error("the file holds the credential in clear text")
	}

	// A new store of the same directory reads the credential back.
	other, err := OpenFile(dir)
	if err != nil {
		t.Fatalf("open the store: %v", err)
	}
	value, ok, err := other.Get("h1", FieldKeyText)
	if err != nil || !ok {
		t.Fatalf("the credential was not found: %v %v", ok, err)
	}
	if value != "-----BEGIN OPENSSH PRIVATE KEY-----" {
		t.Fatalf("unexpected credential %q", value)
	}
}

// TestTheKeyOfTheStoreIsItsOwn makes sure that every store has a key of its
// own.
func TestTheKeyOfTheStoreIsItsOwn(t *testing.T) {
	dir := t.TempDir()
	first, err := OpenFile(dir)
	if err != nil {
		t.Fatalf("open the store: %v", err)
	}
	if err := first.Put("h1", FieldPassword, "s3cret"); err != nil {
		t.Fatalf("put: %v", err)
	}
	key, err := os.ReadFile(filepath.Join(dir, keyFileName))
	if err != nil {
		t.Fatalf("read the key: %v", err)
	}
	if len(key) != keySize {
		t.Fatalf("unexpected key length %d", len(key))
	}
	if strings.Contains(string(key), "s3cret") {
		t.Error("the key holds the credential")
	}
}

// fakeBackend is a storage of credentials for the tests. It can be made to
// refuse the values, like a keyring that cannot hold them.
type fakeBackend struct {
	values map[string]string
	// refuse tells whether the backend refuses the values.
	refuse bool
}

func newFakeBackend() *fakeBackend {
	return &fakeBackend{values: map[string]string{}}
}

func (b *fakeBackend) Get(account string) (string, error) {
	value, ok := b.values[account]
	if !ok {
		return "", ErrNotFound
	}
	return value, nil
}

func (b *fakeBackend) Put(account string, value string) error {
	if b.refuse {
		return ErrTooLarge
	}
	b.values[account] = value
	return nil
}

func (b *fakeBackend) Delete(account string) error {
	delete(b.values, account)
	return nil
}

// TestAnOldValueDoesNotShadowTheNewOne makes sure that a value the keyring
// could not take does not stay in the keyring and shadow the new one.
func TestAnOldValueDoesNotShadowTheNewOne(t *testing.T) {
	dir := t.TempDir()
	file, err := newFileBackend(dir)
	if err != nil {
		t.Fatalf("open the file backend: %v", err)
	}
	keyring := newFakeBackend()
	keyring.values["h1:password"] = "old"
	store := &Store{keyring: keyring, file: file, dir: dir}

	// The keyring cannot take the new value: it goes to the file.
	keyring.refuse = true
	if err := store.Put("h1", FieldPassword, "new"); err != nil {
		t.Fatalf("put: %v", err)
	}
	value, ok, err := store.Get("h1", FieldPassword)
	if err != nil || !ok {
		t.Fatalf("the credential was not found: %v %v", ok, err)
	}
	if value != "new" {
		t.Fatalf("the old value shadows the new one: %q", value)
	}
	if _, ok := keyring.values["h1:password"]; ok {
		t.Error("the old value is still in the keyring")
	}
}

// TestAValueOfTheFileWinsOverTheKeyring makes sure that a value written
// while the keyring was not usable is still the one that is read.
func TestAValueOfTheFileWinsOverTheKeyring(t *testing.T) {
	dir := t.TempDir()
	file, err := newFileBackend(dir)
	if err != nil {
		t.Fatalf("open the file backend: %v", err)
	}
	keyring := newFakeBackend()
	keyring.values["h1:password"] = "old"
	store := &Store{keyring: keyring, file: file, dir: dir}

	// The keyring is not usable: the value goes to the file.
	store.keyring = nil
	if err := store.Put("h1", FieldPassword, "new"); err != nil {
		t.Fatalf("put: %v", err)
	}
	// The keyring works again and still holds the old value.
	store.keyring = keyring
	value, ok, err := store.Get("h1", FieldPassword)
	if err != nil || !ok {
		t.Fatalf("the credential was not found: %v %v", ok, err)
	}
	if value != "new" {
		t.Fatalf("the old value shadows the new one: %q", value)
	}
}

func TestTheKeyringCanBeTurnedOff(t *testing.T) {
	t.Setenv("SSH_TUNNEL_KEYRING", "0")
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open the store: %v", err)
	}
	if store.Backend() != "encrypted file" {
		t.Fatalf("unexpected backend %q", store.Backend())
	}
}
