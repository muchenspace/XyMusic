package main

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"time"

	"xymusic/server/internal/platform/workerstatus"
)

type childResult struct {
	exitCode int
	err      error
}

type childProcess struct {
	role    string
	command *exec.Cmd
	done    chan childResult
}

func runSupervisorProcess(
	ctx context.Context,
	logger *slog.Logger,
	configurationPath string,
) int {
	executable, err := os.Executable()
	if err != nil {
		logger.Error("locate executable", "error", err)
		return 1
	}
	server, err := startChildProcess(executable, "serve")
	if err != nil {
		logger.Error("start server process", "error", err)
		return 1
	}
	worker, workerErr := startChildProcess(executable, "worker")
	if workerErr != nil {
		logger.Warn("start worker process failed", "error", workerErr)
		worker = failedChildProcess(workerErr)
	}
	restartAttempt := 0
	workerStatusPath := configurationPath + ".worker-status"
	for {
		var workerDone <-chan childResult
		if worker != nil {
			workerDone = worker.done
		}
		select {
		case <-ctx.Done():
			stopContext, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			stopChildren(stopContext, server, worker)
			cancel()
			return 0
		case result := <-server.done:
			stopContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			stopChildren(stopContext, worker)
			cancel()
			if result.err != nil {
				logger.Error("server process exited", "exitCode", result.exitCode, "error", result.err)
			}
			return normalizedExitCode(result.exitCode)
		case result := <-workerDone:
			worker = nil
			if result.exitCode == permanentWorkerFailureExitCode {
				logger.Error("worker stopped after permanent migration failure", "exitCode", result.exitCode)
				_ = workerstatus.WriteDocument(context.Background(), workerStatusPath, workerstatus.Document{
					State: "CONFIGURATION_ERROR", UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano),
				})
				continue
			}
			restartAttempt++
			delay := workerRestartDelay(restartAttempt)
			logger.Warn("worker process will restart", "attempt", restartAttempt, "delay", delay, "exitCode", result.exitCode, "error", result.err)
			_ = workerstatus.WriteDocument(context.Background(), workerStatusPath, workerstatus.Document{
				State: "RESTARTING", UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano),
			})
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				stopContext, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				stopChildren(stopContext, server)
				cancel()
				return 0
			case serverResult := <-server.done:
				timer.Stop()
				return normalizedExitCode(serverResult.exitCode)
			case <-timer.C:
			}
			worker, workerErr = startChildProcess(executable, "worker")
			if workerErr != nil {
				logger.Warn("restart worker process failed", "error", workerErr)
				worker = failedChildProcess(workerErr)
			}
		}
	}
}

func startChildProcess(executable, role string) (*childProcess, error) {
	arguments := []string{role}
	command := exec.Command(executable, arguments...)
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	configureChildProcess(command)
	if err := command.Start(); err != nil {
		return nil, err
	}
	child := &childProcess{role: role, command: command, done: make(chan childResult, 1)}
	go func() {
		err := command.Wait()
		exitCode := command.ProcessState.ExitCode()
		child.done <- childResult{exitCode: exitCode, err: err}
		close(child.done)
	}()
	return child, nil
}

func failedChildProcess(err error) *childProcess {
	done := make(chan childResult, 1)
	done <- childResult{exitCode: 1, err: err}
	close(done)
	return &childProcess{role: "worker", done: done}
}

func stopChildren(ctx context.Context, children ...*childProcess) {
	var group sync.WaitGroup
	for _, child := range children {
		if child == nil || child.command == nil || child.command.Process == nil {
			continue
		}
		group.Add(1)
		go func(process *childProcess) {
			defer group.Done()
			_ = interruptChildProcess(process.command.Process)
			select {
			case <-process.done:
				return
			case <-ctx.Done():
				_ = process.command.Process.Kill()
				select {
				case <-process.done:
				case <-time.After(2 * time.Second):
				}
			}
		}(child)
	}
	group.Wait()
}

func workerRestartDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	delay := time.Second << min(attempt-1, 5)
	if delay > 30*time.Second {
		return 30 * time.Second
	}
	return delay
}

func normalizedExitCode(code int) int {
	if code < 0 {
		return 1
	}
	return code
}
