//go:build windows

package singleinstance

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestAcquireAlreadyRunning(t *testing.T) {
	name := fmt.Sprintf(`Local\ssh-tunnel-test-%d`, time.Now().UnixNano())
	first, err := Acquire(name, "")
	if err != nil {
		t.Fatalf("first Acquire: %v", err)
	}
	defer first.Release()

	second, err := Acquire(name, "")
	if !errors.Is(err, ErrAlreadyRunning) {
		if second != nil {
			second.Release()
		}
		t.Fatalf("second Acquire = (%v, %v), want ErrAlreadyRunning", second, err)
	}
	if second != nil {
		second.Release()
		t.Fatal("second Acquire returned a guard")
	}
}
