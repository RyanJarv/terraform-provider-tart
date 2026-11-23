package provider

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// withWriteLock serializes provider writes, including multi-command operations.
func withWriteLock(ctx context.Context, store string, operation func() error) error {
	if resolved, err := filepath.EvalSymlinks(store); err != nil || resolved != store {
		return errors.New("Tart store no longer resolves to the configured directory")
	}
	lock, err := os.OpenFile(filepath.Join(store, ".terraform-tart.lock"),
		os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0600)
	if err != nil {
		return fmt.Errorf("open store lock: %w", err)
	}
	defer lock.Close()
	stat, err := lock.Stat()
	if err != nil || !stat.Mode().IsRegular() {
		return errors.New("Tart store lock must be a regular file")
	}
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return operation()
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EINTR) {
			return fmt.Errorf("lock Tart store: %w", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
		}
	}
}
