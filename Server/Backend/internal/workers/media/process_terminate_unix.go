//go:build !windows

package media

import (
	"errors"
	"os"
	"syscall"
)

func requestProcessTermination(process *os.Process) error {
	if process == nil {
		return os.ErrProcessDone
	}
	err := process.Signal(syscall.SIGTERM)
	if errors.Is(err, os.ErrProcessDone) {
		return os.ErrProcessDone
	}
	return err
}
