// Package tart manages local VMs through the Tart CLI.
package tart

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var (
	vmName = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,62}$`)
	// ErrNotFound means the named VM is absent from an accessible store.
	ErrNotFound = errors.New("VM is absent")
)

// Client runs Tart commands against one local store with automatic pruning disabled.
type Client struct {
	executable string
	store      string
}

// Config selects the executable and existing Tart home directory.
// Empty fields use tart on PATH and TART_HOME (or ~/.tart), respectively.
type Config struct {
	Executable string
	Home       string
}

// NewClient resolves the executable and store once at construction.
func NewClient(config Config) (*Client, error) {
	executable, store := config.Executable, config.Home
	if executable == "" {
		var err error
		executable, err = exec.LookPath("tart")
		if err != nil {
			return nil, fmt.Errorf("find tart on PATH: %w", err)
		}
	}
	if store == "" {
		store = os.Getenv("TART_HOME")
	}
	if store == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		store = filepath.Join(home, ".tart")
	}
	if !filepath.IsAbs(executable) || !filepath.IsAbs(store) {
		return nil, errors.New("Tart executable and home must be absolute paths")
	}
	binary, err := filepath.EvalSymlinks(executable)
	if err != nil {
		return nil, fmt.Errorf("resolve Tart executable: %w", err)
	}
	info, err := os.Stat(binary)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
		return nil, errors.New("Tart executable must resolve to an executable file")
	}
	canonical, err := filepath.EvalSymlinks(store)
	if err != nil {
		return nil, fmt.Errorf("resolve Tart store: %w", err)
	}
	info, err = os.Stat(canonical)
	if err != nil || !info.IsDir() {
		return nil, errors.New("Tart home must resolve to an existing directory")
	}
	return &Client{executable: binary, store: canonical}, nil
}

// Home returns the resolved absolute store path.
func (c *Client) Home() string { return c.store }

// ValidName reports whether name is a supported local VM name.
func ValidName(name string) bool { return vmName.MatchString(name) }

func (c *Client) run(ctx context.Context, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.executable, args...)
	cmd.Env = append(os.Environ(), "TART_HOME="+c.store, "TART_NO_AUTO_PRUNE=1")
	cmd.WaitDelay = 5 * time.Second
	out, err := cmd.Output()
	if ctx.Err() != nil {
		return nil, fmt.Errorf("tart %s: %w", args[0], ctx.Err())
	}
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			msg := strings.TrimSpace(string(exit.Stderr))
			if msg != "" {
				return nil, fmt.Errorf("tart %s: %w: %s", args[0], err, msg)
			}
		}
		return nil, fmt.Errorf("tart %s: %w", args[0], err)
	}
	return out, nil
}

func (c *Client) vmDir(name string) (string, error) {
	if !ValidName(name) {
		return "", fmt.Errorf("invalid local VM name %q", name)
	}
	if live, err := filepath.EvalSymlinks(c.store); err != nil || live != c.store {
		return "", errors.New("Tart store no longer resolves to the configured directory")
	}
	vms := filepath.Join(c.store, "vms")
	info, err := os.Lstat(vms)
	if err == nil && !info.IsDir() {
		return "", errors.New("Tart vms path must be a directory, not a link")
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("inspect Tart vms directory: %w", err)
	}
	return filepath.Join(vms, name), nil
}

func (c *Client) checkVM(name string) error {
	dir, err := c.vmDir(name)
	if err != nil {
		return err
	}
	info, err := os.Lstat(dir)
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%w: %s", ErrNotFound, name)
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("VM path must be a directory")
	}
	config, err := os.Lstat(filepath.Join(dir, "config.json"))
	if err != nil {
		return fmt.Errorf("inspect VM configuration: %w", err)
	}
	if !config.Mode().IsRegular() {
		return errors.New("VM configuration must be a regular file")
	}
	return nil
}
