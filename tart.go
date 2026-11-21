package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	scratchMarker = ".terraform-tart-scratch"
	ownerMarker   = ".terraform-tart-owner"
	scratchValue  = "terraform-provider-tart disposable store v1\n"
)

var (
	vmName        = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,62}$`)
	errVMMissing  = errors.New("VM is absent")
	errVMIdentity = errors.New("VM identity does not match Terraform state")
)

type client struct {
	executable string
	store      string
}

type vmInfo struct {
	OS     string  `json:"OS"`
	CPU    int64   `json:"CPU"`
	Memory int64   `json:"Memory"`
	Disk   float64 `json:"Disk"`
	State  string  `json:"State"`
}

type observedVM struct {
	vmInfo
	id string
}

func newClient(executable, store string) (*client, error) {
	if !filepath.IsAbs(executable) || !filepath.IsAbs(store) {
		return nil, errors.New("tart_path and tart_home must be absolute paths")
	}
	binary, err := filepath.EvalSymlinks(executable)
	if err != nil {
		return nil, fmt.Errorf("resolve Tart executable: %w", err)
	}
	info, err := os.Stat(binary)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
		return nil, errors.New("tart_path must resolve to an executable file")
	}
	canonical, err := filepath.EvalSymlinks(store)
	if err != nil {
		return nil, fmt.Errorf("resolve Tart store: %w", err)
	}
	info, err = os.Stat(canonical)
	if err != nil || !info.IsDir() {
		return nil, errors.New("tart_home must resolve to an existing directory")
	}
	return &client{executable: binary, store: canonical}, nil
}

func (c *client) run(ctx context.Context, args ...string) ([]byte, error) {
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

func (c *client) vmDir(name string) (string, error) {
	if !vmName.MatchString(name) {
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

func (c *client) identity(name string) (string, error) {
	dir, err := c.vmDir(name)
	if err != nil {
		return "", err
	}
	info, err := os.Lstat(dir)
	if errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("%w: %s", errVMMissing, name)
	}
	if err != nil {
		return "", fmt.Errorf("inspect VM directory: %w", err)
	}
	if !info.IsDir() {
		return "", errors.New("VM directory must not be a link")
	}
	config, err := os.Lstat(filepath.Join(dir, "config.json"))
	if err != nil {
		return "", fmt.Errorf("inspect VM configuration: %w", err)
	}
	if !config.Mode().IsRegular() {
		return "", errors.New("VM configuration must be a regular file")
	}
	key, err := fileIdentity(info)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(c.store + "\x00" + name + "\x00" + key))
	return hex.EncodeToString(sum[:]), nil
}

func (c *client) get(ctx context.Context, name string) (observedVM, error) {
	id, err := c.identity(name)
	if err != nil {
		return observedVM{}, err
	}
	out, err := c.run(ctx, "get", name, "--format", "json")
	if err != nil {
		return observedVM{}, err
	}
	var info vmInfo
	if err := json.Unmarshal(out, &info); err != nil {
		return observedVM{}, fmt.Errorf("decode Tart VM configuration: %w", err)
	}
	if (info.OS != "darwin" && info.OS != "linux") ||
		info.CPU < 1 || info.Memory < 1 || info.Disk <= 0 {
		return observedVM{}, errors.New("Tart returned incomplete or unsupported VM configuration")
	}
	switch info.State {
	case "stopped", "running", "suspended":
	default:
		return observedVM{}, fmt.Errorf("unsupported Tart VM state %q", info.State)
	}
	if err := c.verifyIdentity(name, id); err != nil {
		return observedVM{}, err
	}
	return observedVM{vmInfo: info, id: id}, nil
}

func (c *client) verifyIdentity(name, expected string) error {
	id, err := c.identity(name)
	if err != nil {
		return err
	}
	if id != expected {
		return errVMIdentity
	}
	return nil
}

func (c *client) withWriteLock(ctx context.Context, operation func() error) error {
	if _, err := c.vmDir("terraform-lock-check"); err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(c.store, ".terraform-tart.lock"),
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

func (c *client) resize(ctx context.Context, name string, current observedVM, cpu, memory int64) (observedVM, error) {
	if cpu == current.CPU && memory == current.Memory {
		return current, nil
	}
	if current.State != "stopped" {
		return observedVM{}, fmt.Errorf("VM %q must be stopped before resizing; current state is %s", name, current.State)
	}
	if _, err := c.run(ctx, "set", name, "--cpu", strconv.FormatInt(cpu, 10),
		"--memory", strconv.FormatInt(memory, 10)); err != nil {
		return observedVM{}, err
	}
	next, err := c.get(ctx, name)
	if err != nil {
		return observedVM{}, err
	}
	if next.id != current.id {
		return observedVM{}, errVMIdentity
	}
	if next.CPU != cpu || next.Memory != memory {
		return observedVM{}, errors.New("Tart did not report the requested CPU and memory settings")
	}
	return next, nil
}

func readMarker(path, expected string) error {
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&022 != 0 ||
		info.Size() != int64(len(expected)) {
		return errors.New("invalid ownership marker")
	}
	content := make([]byte, len(expected))
	if _, err := file.ReadAt(content, 0); err != nil {
		return err
	}
	if string(content) != expected {
		return errors.New("ownership marker does not match")
	}
	return nil
}

func (c *client) requireScratch() error {
	if err := readMarker(filepath.Join(c.store, scratchMarker), scratchValue); err != nil {
		return fmt.Errorf("disposable VMs require an explicitly marked scratch store: %w", err)
	}
	return nil
}

func (c *client) recordOwnership(name, id string) error {
	dir, err := c.vmDir(name)
	if err != nil {
		return err
	}
	path := filepath.Join(dir, ownerMarker)
	value, err := disposableIdentity(dir, id)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, writeErr := file.WriteString(value)
	return errors.Join(writeErr, file.Close())
}

func (c *client) checkOwnership(name, id string) error {
	if err := c.requireScratch(); err != nil {
		return err
	}
	if err := c.verifyIdentity(name, id); err != nil {
		return err
	}
	dir, err := c.vmDir(name)
	if err != nil {
		return err
	}
	value, err := disposableIdentity(dir, id)
	if err != nil {
		return err
	}
	return readMarker(filepath.Join(dir, ownerMarker), value)
}

func disposableIdentity(dir, id string) (string, error) {
	disk, err := os.Lstat(filepath.Join(dir, "disk.img"))
	if err != nil {
		return "", fmt.Errorf("inspect disposable VM disk: %w", err)
	}
	if !disk.Mode().IsRegular() {
		return "", errors.New("disposable VM disk must be a regular file")
	}
	diskID, err := fileIdentity(disk)
	if err != nil {
		return "", err
	}
	return id + "\n" + diskID + "\n", nil
}
