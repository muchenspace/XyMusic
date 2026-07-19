//go:build windows

package media

import "os"

func requestProcessTermination(process *os.Process) error {
	if process == nil {
		return os.ErrProcessDone
	}
	return process.Kill()
}
