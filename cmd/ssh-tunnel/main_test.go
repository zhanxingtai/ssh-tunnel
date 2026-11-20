package main

import (
	"testing"

	"ssh-tunnel/internal/config"
)

// TestInterfaceAddr checks the order of the sources of the address of the
// interface: the command line wins over the configuration file, which wins
// over the default address.
func TestInterfaceAddr(t *testing.T) {
	cfg := config.Default()
	cfg.Settings.Addr = "127.0.0.1:9090"

	// The configuration file is used when the command line says nothing.
	got, err := interfaceAddr(flags{}, cfg)
	if err != nil {
		t.Fatalf("resolve the address: %v", err)
	}
	if got != "127.0.0.1:9090" {
		t.Errorf("the address is %q", got)
	}

	// The command line wins over the configuration file.
	f, err := parseFlags([]string{"--addr", "0.0.0.0:8080"})
	if err != nil {
		t.Fatalf("parse the flags: %v", err)
	}
	got, err = interfaceAddr(f, cfg)
	if err != nil {
		t.Fatalf("resolve the address: %v", err)
	}
	if got != "0.0.0.0:8080" {
		t.Errorf("the address is %q", got)
	}

	// The default address is used when nothing is configured.
	empty := config.Default()
	got, err = interfaceAddr(flags{}, empty)
	if err != nil {
		t.Fatalf("resolve the address: %v", err)
	}
	if got != config.DefaultInterfaceAddr {
		t.Errorf("the address is %q", got)
	}
}

// TestInterfaceAddrRefusesABrokenAddress makes sure that a broken address
// stops the program instead of being silently ignored.
func TestInterfaceAddrRefusesABrokenAddress(t *testing.T) {
	cfg := config.Default()
	cfg.Settings.Addr = "127.0.0.1"
	if _, err := interfaceAddr(flags{}, cfg); err == nil {
		t.Error("a broken address was accepted")
	}
	f, err := parseFlags([]string{"--addr", "not-an-address"})
	if err != nil {
		t.Fatalf("parse the flags: %v", err)
	}
	if _, err := interfaceAddr(f, config.Default()); err == nil {
		t.Error("a broken address was accepted")
	}
}

// TestDaemonOpensTheInterfaceOnlyWhenAnAddressIsNamed makes sure that the
// daemon does not open the interface on a machine without a desktop, unless
// an address was named.
func TestDaemonOpensTheInterfaceOnlyWhenAnAddressIsNamed(t *testing.T) {
	cfg := config.Default()
	if daemonOpensInterface(flags{}, cfg) {
		t.Error("the daemon opened the interface without an address")
	}
	cfg.Settings.Addr = "127.0.0.1:9090"
	if !daemonOpensInterface(flags{}, cfg) {
		t.Error("the daemon did not open the interface with an address")
	}
	// The command line alone is enough.
	empty := config.Default()
	f, err := parseFlags([]string{"--addr", "127.0.0.1:9090"})
	if err != nil {
		t.Fatalf("parse the flags: %v", err)
	}
	if !daemonOpensInterface(f, empty) {
		t.Error("the daemon did not open the interface with an address on the command line")
	}
}

// TestParseFlagsRemembersTheAddress makes sure that an address given on the
// command line is told apart from the default one.
func TestParseFlagsRemembersTheAddress(t *testing.T) {
	f, err := parseFlags(nil)
	if err != nil {
		t.Fatalf("parse the flags: %v", err)
	}
	if f.addrGiven {
		t.Error("an address was reported that was not given")
	}
	f, err = parseFlags([]string{"--addr", "127.0.0.1:1234"})
	if err != nil {
		t.Fatalf("parse the flags: %v", err)
	}
	if !f.addrGiven {
		t.Error("the address was not reported")
	}
}
