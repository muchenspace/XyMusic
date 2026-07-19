//go:build windows

package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

const supervisorSignalHelperEnvironment = "XYMUSIC_SUPERVISOR_SIGNAL_HELPER"

func TestConfigureChildProcessCreatesDedicatedProcessGroup(t *testing.T) {
	command := exec.Command(os.Args[0])
	configureChildProcess(command)
	if command.SysProcAttr == nil || command.SysProcAttr.CreationFlags&windows.CREATE_NEW_PROCESS_GROUP == 0 {
		t.Fatal("supervisor child was not assigned a dedicated Windows process group")
	}
}

func TestStopChildrenGracefullyInterruptsWindowsProcessGroup(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "stopped")
	command := exec.Command(os.Args[0], "-test.run=^TestSupervisorSignalHelper$")
	command.Env = append(os.Environ(), supervisorSignalHelperEnvironment+"="+marker)
	ready, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	command.Stderr = os.Stderr
	configureChildProcess(command)
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	result := make(chan childResult, 1)
	child := &childProcess{role: "signal-helper", command: command, done: result}
	go func() {
		err := command.Wait()
		result <- childResult{exitCode: command.ProcessState.ExitCode(), err: err}
		close(result)
	}()
	defer func() {
		if command.ProcessState == nil {
			_ = command.Process.Kill()
			<-result
		}
	}()

	buffer := make([]byte, len("READY\n"))
	if _, err := io.ReadFull(ready, buffer); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(buffer)) != "READY" {
		t.Fatalf("helper readiness = %q", buffer)
	}

	started := time.Now()
	stopContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	stopChildren(stopContext, child)
	cancel()
	if elapsed := time.Since(started); elapsed >= 4*time.Second {
		t.Fatalf("graceful child stop took %s", elapsed)
	}
	if command.ProcessState == nil || command.ProcessState.ExitCode() != 0 {
		t.Fatalf("helper process state = %v", command.ProcessState)
	}
	content, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("helper did not persist graceful-stop evidence: %v", err)
	}
	if string(content) != "INTERRUPTED" {
		t.Fatalf("graceful-stop evidence = %q", content)
	}
}

func TestSupervisorSignalHelper(t *testing.T) {
	marker := os.Getenv(supervisorSignalHelperEnvironment)
	if marker == "" {
		return
	}
	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt)
	defer signal.Stop(interrupts)
	fmt.Println("READY")
	select {
	case <-interrupts:
		if err := os.WriteFile(marker, []byte("INTERRUPTED"), 0o600); err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("helper did not receive Windows console interrupt")
	}
}
