package rsdgroup

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/golang/glog"
	"github.com/rbln-sw/rblnlib-go/pkg/rblnsmi"
	"golang.org/x/sys/unix"
)

const (
	rsdDevice          = "/dev/rsd"
	defaultRsdDevice   = rsdDevice + "0"
	lockPollInterval   = 100 * time.Millisecond
	lockFilePerm       = 0o644
	rsdGroupLockFile   = "/var/run/rbln-rsd-group.lock"
	defaultExecTimeout = 5 * time.Second
)

// RecreateRsdGroup removes existing groups for the given devices, then creates
// a new group that includes those devices.
// It returns a /dev/rsd* path, or /dev/rsd0 on failure.
func RecreateRsdGroup(deviceIDs []string) string {
	ctx, cancel := context.WithTimeout(context.Background(), defaultExecTimeout)
	defer cancel()

	groupID, err := withRsdLock(ctx, func() (string, error) {
		if err := rblnsmi.DestroyRsdGroup(ctx, deviceIDs); err != nil {
			glog.Errorf("Failed to destroy RSD groups: %q", err)
			return "", err
		}
		return rblnsmi.CreateRsdGroup(ctx, deviceIDs)
	})

	if err != nil {
		glog.Errorf("Failed to create RSD groups: %q", err)
		return defaultRsdDevice
	}
	return rsdDevice + groupID
}

func withRsdLock(ctx context.Context, fn func() (string, error)) (string, error) {
	release, err := acquireFileLock(ctx, rsdGroupLockFile)
	if err != nil {
		return "", err
	}
	defer func() {
		if relErr := release(); err == nil && relErr != nil {
			err = relErr
		}
	}()
	return fn()
}

func acquireFileLock(ctx context.Context, lockPath string) (release func() error, err error) {
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, lockFilePerm)
	if err != nil {
		return nil, fmt.Errorf("open lock file: %w", err)
	}

	unlock := func() error {
		_ = unix.Flock(int(f.Fd()), unix.LOCK_UN)
		return f.Close()
	}

	ticker := time.NewTicker(lockPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			_ = f.Close()
			return nil, fmt.Errorf("acquire lock timeout/canceled: %w", ctx.Err())
		default:
			if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
				<-ticker.C
				continue
			}
			return unlock, nil
		}
	}
}
