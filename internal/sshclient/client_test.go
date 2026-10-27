package sshclient

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"path/filepath"
	"testing"

	"ssh-tunnel/internal/config"
)

func testPrivateKey(t *testing.T) string {
	t.Helper()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
}

func TestAuthMethodsUsesTheKeyTextBeforeThePath(t *testing.T) {
	methods, err := authMethods(config.Auth{
		Type:    config.AuthKey,
		KeyPath: filepath.Join(t.TempDir(), "missing"),
		KeyText: "\n" + testPrivateKey(t) + "\n",
	}, "")
	if err != nil {
		t.Fatalf("auth methods: %v", err)
	}
	if len(methods) != 1 {
		t.Fatalf("unexpected auth methods: %d", len(methods))
	}
}

func TestAuthMethodsNeedsAPathOrText(t *testing.T) {
	if _, err := authMethods(config.Auth{Type: config.AuthKey, KeyText: "  \n"}, ""); err == nil {
		t.Fatal("expected an error for a host without key path or text")
	}
}
