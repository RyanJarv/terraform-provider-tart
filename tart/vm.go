package tart

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
)

// VM contains the saved configuration and runtime state reported by tart get.
type VM struct {
	OS        string  `json:"OS"`
	CPUCount  int64   `json:"CPU"`
	MemoryMiB int64   `json:"Memory"`
	DiskGB    float64 `json:"Disk"`
	State     string  `json:"State"`
}

// Get reads the VM at name. Use errors.Is(err, ErrNotFound) to check for absence.
func (c *Client) Get(ctx context.Context, name string) (VM, error) {
	if err := c.checkVM(name); err != nil {
		return VM{}, err
	}
	out, err := c.run(ctx, "get", name, "--format", "json")
	if err != nil {
		return VM{}, err
	}
	var info VM
	if err := json.Unmarshal(out, &info); err != nil {
		return VM{}, fmt.Errorf("decode Tart VM configuration: %w", err)
	}
	if (info.OS != "darwin" && info.OS != "linux") ||
		info.CPUCount < 1 || info.MemoryMiB < 1 || info.DiskGB <= 0 {
		return VM{}, errors.New("Tart returned incomplete or unsupported VM configuration")
	}
	switch info.State {
	case "stopped", "running", "suspended":
	default:
		return VM{}, fmt.Errorf("unsupported Tart VM state %q", info.State)
	}
	return info, nil
}

// Size specifies a VM's CPU count and memory in MiB.
type Size struct {
	CPUCount  int64
	MemoryMiB int64
}

// Resize applies the saved CPU and memory settings and returns the observed VM.
// Running guests use their new settings after restart.
func (c *Client) Resize(ctx context.Context, name string, size Size) (VM, error) {
	if size.CPUCount < 1 || size.MemoryMiB < 1 {
		return VM{}, errors.New("CPU count and memory in MiB must be positive")
	}
	current, err := c.Get(ctx, name)
	if err != nil {
		return VM{}, err
	}

	if size.CPUCount == current.CPUCount && size.MemoryMiB == current.MemoryMiB {
		return current, nil
	}
	if _, err := c.run(ctx, "set", name, "--cpu", strconv.FormatInt(size.CPUCount, 10),
		"--memory", strconv.FormatInt(size.MemoryMiB, 10)); err != nil {
		return VM{}, err
	}
	next, err := c.Get(ctx, name)
	if err != nil {
		return VM{}, err
	}
	if next.CPUCount != size.CPUCount || next.MemoryMiB != size.MemoryMiB {
		return VM{}, errors.New("Tart did not report the requested CPU and memory settings")
	}
	return next, nil
}

// Clone copies a stopped local VM to an unused name in the same store.
// An occupied destination returns an error matching os.ErrExist.
func (c *Client) Clone(ctx context.Context, source, name string) error {
	if source == name {
		return errors.New("source and destination must have different names")
	}
	base, err := c.Get(ctx, source)
	if err != nil {
		return fmt.Errorf("inspect source %q: %w", source, err)
	}
	if base.State != "stopped" {
		return fmt.Errorf("source image %q must be stopped", source)
	}
	err = c.checkVM(name)
	if err == nil {
		return fmt.Errorf("destination %q: %w", name, os.ErrExist)
	}
	if !errors.Is(err, ErrNotFound) {
		return err
	}
	if _, err := c.run(ctx, "clone", source, name); err != nil {
		return fmt.Errorf("inspect destination %q before retrying: %w", name, err)
	}
	return nil
}

// Delete removes the local VM at name. A confirmed absence also succeeds.
func (c *Client) Delete(ctx context.Context, name string) error {
	if err := c.checkVM(name); err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		return err
	}
	if _, err := c.run(ctx, "delete", name); err != nil {
		return err
	}
	err := c.checkVM(name)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	return fmt.Errorf("VM %q still exists after tart delete", name)
}
