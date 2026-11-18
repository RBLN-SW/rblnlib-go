package host

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	system "github.com/rbln-sw/rblnlib-go/pkg/system/v1"
	"golang.org/x/sys/unix"
)

func TestAttributeEAGAINReturnsInsteadOfWaitingForPoll(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pci_addr")
	if err := unix.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}
	writer, err := unix.Open(path, unix.O_RDWR|unix.O_NONBLOCK, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if writer >= 0 {
			_ = unix.Close(writer)
		}
	}()
	done := make(chan error, 1)
	go func() { _, err := readText(path); done <- err }()
	select {
	case err := <-done:
		if !errors.Is(err, system.ErrNotReady) || !errors.Is(err, unix.EAGAIN) {
			t.Fatalf("expected transient attribute error, got %v", err)
		}
	case <-time.After(time.Second):
		// Release a regressed os.ReadFile implementation before failing.
		_ = unix.Close(writer)
		writer = -1
		t.Fatal("EAGAIN blocked in the runtime poller")
	}
}

func TestAttributeReadIsCompleteAndBounded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "attribute")
	value := strings.Repeat("a", 8193)
	if err := os.WriteFile(path, []byte(value+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := readText(path); err != nil || got != value {
		t.Fatalf("partial attribute %d: %v", len(got), err)
	}
	if err := os.WriteFile(path, []byte(strings.Repeat("a", (1<<20)+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readText(path); !errors.Is(err, system.ErrInvalidData) {
		t.Fatalf("oversized attribute accepted: %v", err)
	}
}
