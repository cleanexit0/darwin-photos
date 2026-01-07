//go:build darwin

package cmd

import (
	"syscall"
)

type syscallStatfs = syscall.Statfs_t

func statfs(path string, stat *syscallStatfs) error {
	return syscall.Statfs(path, stat)
}
