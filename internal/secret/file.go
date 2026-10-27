package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

const (
	// keyFileName is the file holding the key of the storage.
	keyFileName = "secrets.key"
	// dataFileName is the file holding the encrypted credentials.
	dataFileName = "secrets.enc"
	// keySize is the size of the key in bytes.
	keySize = 32
	// fileVersion is the version of the format of the encrypted file.
	fileVersion = 1
)

// fileBackend keeps the credentials in an encrypted file next to the
// configuration file.
//
// The file is sealed with AES-GCM and a key of its own. The key sits next to
// the file, so the storage protects against a disclosure of the
// configuration file alone, not against a program running as the same user.
type fileBackend struct {
	dir string
	key []byte
	mu  sync.Mutex
}

// fileData is the content of the encrypted file.
type fileData struct {
	// Version is the format of the file.
	Version int `json:"version"`
	// Entries holds the sealed credentials, indexed by account.
	Entries map[string]string `json:"entries"`
}

// newFileBackend returns the storage of a directory. The key of the storage
// is only created when a credential really has to be written.
func newFileBackend(dir string) (*fileBackend, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return &fileBackend{dir: dir}, nil
}

// keyOf returns the key of the storage and creates it when it is missing.
func (b *fileBackend) keyOf() ([]byte, error) {
	if b.key != nil {
		return b.key, nil
	}
	key, err := loadKey(filepath.Join(b.dir, keyFileName))
	if err != nil {
		return nil, err
	}
	b.key = key
	return key, nil
}

// loadKey reads the key of the storage and creates it when it is missing.
func loadKey(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		if len(data) != keySize {
			return nil, errors.New("the key of the credential store is not valid")
		}
		return data, nil
	}
	if !os.IsNotExist(err) {
		return nil, err
	}
	key := make([]byte, keySize)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, key, 0o600); err != nil {
		return nil, err
	}
	return key, nil
}

// Get returns the value of an account.
func (b *fileBackend) Get(account string) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	data, err := b.read()
	if err != nil {
		return "", err
	}
	payload, ok := data.Entries[account]
	if !ok {
		return "", ErrNotFound
	}
	raw, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return "", err
	}
	key, err := b.keyOf()
	if err != nil {
		return "", err
	}
	return unseal(key, raw)
}

// Put stores the value of an account.
func (b *fileBackend) Put(account string, value string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	data, err := b.read()
	if err != nil {
		return err
	}
	key, err := b.keyOf()
	if err != nil {
		return err
	}
	raw, err := seal(key, []byte(value))
	if err != nil {
		return err
	}
	if data.Entries == nil {
		data.Entries = map[string]string{}
	}
	data.Entries[account] = base64.StdEncoding.EncodeToString(raw)
	return b.write(data)
}

// Delete removes an account.
func (b *fileBackend) Delete(account string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	data, err := b.read()
	if err != nil {
		return err
	}
	if _, ok := data.Entries[account]; !ok {
		return nil
	}
	delete(data.Entries, account)
	return b.write(data)
}

// read returns the content of the encrypted file. A missing file is an empty
// set of credentials.
func (b *fileBackend) read() (fileData, error) {
	data, err := os.ReadFile(filepath.Join(b.dir, dataFileName))
	if os.IsNotExist(err) {
		return fileData{Version: fileVersion, Entries: map[string]string{}}, nil
	}
	if err != nil {
		return fileData{}, err
	}
	var content fileData
	if err := json.Unmarshal(data, &content); err != nil {
		return fileData{}, err
	}
	if content.Entries == nil {
		content.Entries = map[string]string{}
	}
	return content, nil
}

// write replaces the content of the encrypted file.
func (b *fileBackend) write(content fileData) error {
	content.Version = fileVersion
	data, err := json.MarshalIndent(content, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	path := filepath.Join(b.dir, dataFileName)
	// The temporary file has a name of its own, so that two programs
	// writing at the same time do not fight over the same file.
	tmp, err := os.CreateTemp(b.dir, dataFileName+".*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

// seal encrypts a value. A fresh nonce is used for every value.
func seal(key, value []byte) ([]byte, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, value, nil), nil
}

// unseal decrypts a value sealed by seal.
func unseal(key, data []byte) (string, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return "", err
	}
	if len(data) < gcm.NonceSize() {
		return "", errors.New("the sealed credential is truncated")
	}
	nonce, value := data[:gcm.NonceSize()], data[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, value, nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

// newGCM returns the cipher that seals the credentials.
func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
