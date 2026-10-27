package main

import (
	"strings"
	"testing"
)

// TestSecretFromReader makes sure that the credential read from the standard
// input loses the line break of the shell.
func TestSecretFromReader(t *testing.T) {
	cases := map[string]string{
		"s3cret\n":     "s3cret",
		"s3cret\r\n":   "s3cret",
		"s3cret":       "s3cret",
		"s3cret\n\n":   "s3cret\n",
		"":             "",
		"two\nlines\n": "two\nlines",
	}
	for in, want := range cases {
		got, err := secretFromReader(strings.NewReader(in))
		if err != nil {
			t.Fatalf("read %q: %v", in, err)
		}
		if got != want {
			t.Errorf("read %q = %q, want %q", in, got, want)
		}
	}
}

// TestSecretValueReadsTheStandardInput makes sure that the value "-" reads the
// credential from the standard input.
func TestSecretValueReadsTheStandardInput(t *testing.T) {
	got, err := secretValue("plain")
	if err != nil {
		t.Fatalf("read the value: %v", err)
	}
	if got != "plain" {
		t.Errorf("unexpected value %q", got)
	}
}

// TestSecretFromEnv makes sure that a credential is read from the environment.
func TestSecretFromEnv(t *testing.T) {
	t.Setenv("SSH_TUNNEL_TEST_PASSWORD", "s3cret")
	got, err := secretFromEnv("SSH_TUNNEL_TEST_PASSWORD")
	if err != nil {
		t.Fatalf("read the environment: %v", err)
	}
	if got != "s3cret" {
		t.Errorf("unexpected value %q", got)
	}
	if _, err := secretFromEnv("SSH_TUNNEL_TEST_MISSING"); err == nil {
		t.Error("a missing environment variable was accepted")
	}
}
