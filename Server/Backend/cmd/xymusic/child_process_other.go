//go:build !windows

package main

import (
	"os"
	"os/exec"
)

func configureChildProcess(*exec.Cmd) {}

func interruptChildProcess(process *os.Process) error {
	return process.Signal(os.Interrupt)
}
