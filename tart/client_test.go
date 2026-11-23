package tart_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ryanjarv/terraform-provider-tart/tart"
)

const fixtureJSON = `{"OS":"linux","CPU":4,"Memory":8192,"Disk":64,"State":"stopped"}`

func fakeVM(t *testing.T) (*tart.Client, string) {
	t.Helper()
	store := t.TempDir()
	binary := filepath.Join(store, "tart")
	script := `#!/bin/sh
set -eu
[ "$TART_NO_AUTO_PRUNE" = 1 ]
[ "$#" = 4 ]
[ "$1" = get ]
[ "$3" = --format ]
[ "$4" = json ]
cat "$TART_HOME/vms/$2/config.json"
`
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	c, err := tart.NewClient(tart.Config{Executable: binary, Home: store})
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(c.Home(), "vms", "fixture")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(fixtureJSON), 0600); err != nil {
		t.Fatal(err)
	}
	return c, dir
}

func TestLookupAndFailClosed(t *testing.T) {
	c, dir := fakeVM(t)
	t.Setenv("TART_HOME", "/unexpected")
	t.Setenv("TART_NO_AUTO_PRUNE", "unused")
	vm, err := c.Get(context.Background(), "fixture")
	if err != nil || vm.CPUCount != 4 || vm.MemoryMiB != 8192 || vm.DiskGB != 64 || vm.State != "stopped" {
		t.Fatalf("unexpected lookup: %#v, %v", vm, err)
	}
	if _, err := c.Get(context.Background(), "missing"); !errors.Is(err, tart.ErrNotFound) {
		t.Fatalf("expected absent VM error, got %v", err)
	}
	if _, err := c.Get(context.Background(), "../fixture"); err == nil {
		t.Fatal("accepted a path as a VM name")
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"State":"stopped"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Get(context.Background(), "fixture"); err == nil || errors.Is(err, tart.ErrNotFound) {
		t.Fatalf("invalid JSON schema must fail without marking VM absent: %v", err)
	}
}

// A caller can distinguish a collision, a missing source, and an unreadable VM.
func TestClientErrors(t *testing.T) {
	c, dir := fakeVM(t)
	ctx := context.Background()
	if err := c.Delete(ctx, "missing"); err != nil {
		t.Fatalf("delete missing VM: %v", err)
	}
	if err := c.Clone(ctx, "missing", "clone"); !errors.Is(err, tart.ErrNotFound) {
		t.Fatalf("expected a missing source, got %v", err)
	}
	if err := os.Mkdir(filepath.Join(c.Home(), "vms", "clone"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(c.Home(), "vms", "clone", "config.json"), []byte(fixtureJSON), 0600); err != nil {
		t.Fatal(err)
	}
	if err := c.Clone(ctx, "fixture", "clone"); !errors.Is(err, os.ErrExist) {
		t.Fatalf("expected an occupied destination, got %v", err)
	}
	if err := os.Remove(filepath.Join(dir, "config.json")); err != nil {
		t.Fatal(err)
	}
	if err := c.Delete(ctx, "fixture"); err == nil || errors.Is(err, tart.ErrNotFound) {
		t.Fatalf("expected an incomplete store entry to fail, got %v", err)
	}
}
