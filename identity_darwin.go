//go:build darwin

package main

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

func fileIdentity(info os.FileInfo) (string, error) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Birthtimespec.Sec <= 0 {
		return "", errors.New("cannot establish the VM file's creation identity")
	}
	return fmt.Sprintf("%d:%d:%d:%d", stat.Dev, stat.Ino,
		stat.Birthtimespec.Sec, stat.Birthtimespec.Nsec), nil
}
