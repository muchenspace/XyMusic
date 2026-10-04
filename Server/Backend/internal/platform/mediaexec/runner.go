// Package mediaexec runs external media tools (ffprobe/ffmpeg) with a
// timeout and reports their exit state without treating a non-zero exit as a
// transport error. The media inspectors share this contract.
package mediaexec

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"time"
)

type CommandResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
	TimedOut bool
}

type CommandRunner interface {
	Run(context.Context, string, []string, time.Duration) (CommandResult, error)
}

// OSCommandRunner executes the command on the host OS. A timeout is reported
// through CommandResult.TimedOut with ExitCode -1, and a non-zero exit code is
// reported through CommandResult.ExitCode without an error.
type OSCommandRunner struct{}

func (OSCommandRunner) Run(
	ctx context.Context,
	executable string,
	arguments []string,
	timeout time.Duration,
) (CommandResult, error) {
	commandContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	command := exec.CommandContext(commandContext, executable, arguments...)
	var stdout strings.Builder
	var stderr strings.Builder
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	result := CommandResult{Stdout: stdout.String(), Stderr: stderr.String()}
	if errors.Is(commandContext.Err(), context.DeadlineExceeded) {
		result.TimedOut = true
		result.ExitCode = -1
		return result, nil
	}
	if err == nil {
		return result, nil
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		result.ExitCode = exitError.ExitCode()
		return result, nil
	}
	return CommandResult{}, err
}
