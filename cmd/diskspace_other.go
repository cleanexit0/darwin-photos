//go:build !darwin

package cmd

import "errors"

type syscallStatfs struct {
	Bavail uint64
	Bsize  int64
}

func statfs(path string, stat *syscallStatfs) error {
	return errors.New("disk space check not supported on this platform")
}
