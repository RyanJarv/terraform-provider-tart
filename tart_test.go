//go:build darwin

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const fixtureJSON = `{"OS":"linux","CPU":4,"Memory":8192,"Disk":64,"State":"stopped"}`

func fakeVM(t *testing.T) (*client, string) {
	t.Helper()
	store := t.TempDir()
	binary := filepath.Join(store, "tart")
	script := `#!/bin/sh
set -eu
[ "$TART_NO_AUTO_PRUNE" = 1 ]
[ "$#" = 4 ] && [ "$1" = get ] && [ "$3" = --format ] && [ "$4" = json ]
cat "$TART_HOME/vms/$2/config.json"
`
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	c, err := newClient(binary, store)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(c.store, "vms", "fixture")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for name, contents := range map[string]string{"config.json": fixtureJSON, "disk.img": "fixture disk"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return c, dir
}

func TestLookupAndFailClosed(t *testing.T) {
	c, dir := fakeVM(t)
	t.Setenv("TART_HOME", "/unexpected")
	t.Setenv("TART_NO_AUTO_PRUNE", "unused")
	vm, err := c.get(context.Background(), "fixture")
	if err != nil || vm.CPU != 4 || vm.Memory != 8192 || vm.Disk != 64 || vm.State != "stopped" || vm.id == "" {
		t.Fatalf("unexpected lookup: %#v, %v", vm, err)
	}
	if _, err := c.get(context.Background(), "missing"); !errors.Is(err, errVMMissing) {
		t.Fatalf("expected absent VM error, got %v", err)
	}
	if _, err := c.get(context.Background(), "../fixture"); err == nil {
		t.Fatal("accepted a path as a VM name")
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"State":"stopped"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := c.get(context.Background(), "fixture"); err == nil || errors.Is(err, errVMMissing) {
		t.Fatalf("invalid JSON schema must fail without marking VM absent: %v", err)
	}
}

func TestReplacementAndDiskOwnership(t *testing.T) {
	c, dir := fakeVM(t)
	id, err := c.identity("fixture")
	if err != nil {
		t.Fatal(err)
	}
	if err := c.checkOwnership("fixture", id); err == nil {
		t.Fatal("allowed deletion in an unmarked store")
	}
	if err := os.WriteFile(filepath.Join(c.store, scratchMarker), []byte(scratchValue), 0600); err != nil {
		t.Fatal(err)
	}
	if err := c.recordOwnership("fixture", id); err != nil {
		t.Fatal(err)
	}
	if err := c.checkOwnership("fixture", id); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(dir, "disk.img"), filepath.Join(dir, "original.img")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "disk.img"), []byte("new disk"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := c.checkOwnership("fixture", id); err == nil || !strings.Contains(err.Error(), "marker") {
		t.Fatalf("replacement disk must fail ownership check: %v", err)
	}
	if err := os.Rename(dir, dir+"-original"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(fixtureJSON), 0600); err != nil {
		t.Fatal(err)
	}
	if err := c.verifyIdentity("fixture", id); !errors.Is(err, errVMIdentity) {
		t.Fatalf("same-name replacement must have a different identity: %v", err)
	}
}
