//go:build windows

package main

import (
	"errors"
	"os"
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

func configureChildProcess(command *exec.Cmd) {
	if command.SysProcAttr == nil {
		command.SysProcAttr = &syscall.SysProcAttr{}
	}
	command.SysProcAttr.CreationFlags |= windows.CREATE_NEW_PROCESS_GROUP
}

func interruptChildProcess(process *os.Process) error {
	if process == nil || process.Pid <= 0 || uint64(process.Pid) > uint64(^uint32(0)) {
		return errors.New("invalid child process")
	}
	return windows.GenerateConsoleCtrlEvent(windows.CTRL_BREAK_EVENT, uint32(process.Pid))
}
