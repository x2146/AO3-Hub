//go:build !windows

package app

import (
	"os"
	"syscall"
)

func execCurrentProcess(execPath string) error {
	return syscall.Exec(execPath, os.Args, os.Environ())
}
