//go:build !darwin

package main

import (
	"errors"
	"os"
)

func fileIdentity(os.FileInfo) (string, error) {
	return "", errors.New("VM identity requires Tart on macOS")
}
