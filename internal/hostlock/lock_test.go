package hostlock

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestLockWaitHonorsDeadlineAndReleases(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lock")
	release, err := Acquire(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, err := Acquire(ctx, path); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("contending lock: %v", err)
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
	release, err = Acquire(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
}
