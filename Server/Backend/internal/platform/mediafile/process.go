package mediafile

import (
	"context"
	"errors"
	"os/exec"
	"time"

	"xymusic/server/internal/platform/processio"
)

type ProcessResult struct {
	Stdout          string
	Stderr          string
	ExitCode        int
	TimedOut        bool
	StdoutTruncated bool
}

type ProcessRunner interface {
	Run(context.Context, string, []string, time.Duration) (ProcessResult, error)
}

type OSProcessRunner struct{}

func (OSProcessRunner) Run(
	ctx context.Context,
	executable string,
	arguments []string,
	timeout time.Duration,
) (ProcessResult, error) {
	commandContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	command := exec.CommandContext(commandContext, executable, arguments...)
	command.WaitDelay = 5 * time.Second
	stdout := processio.NewHeadBuffer(maximumToolStdout)
	stderr := processio.NewTailBuffer(maximumToolStderr)
	command.Stdout = stdout
	command.Stderr = stderr
	err := command.Run()
	result := ProcessResult{
		Stdout: stdout.String(), Stderr: stderr.String(), StdoutTruncated: stdout.Truncated(),
	}
	if ctx.Err() != nil {
		if cause := context.Cause(ctx); cause != nil {
			return ProcessResult{}, cause
		}
		return ProcessResult{}, ctx.Err()
	}
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
	return ProcessResult{}, err
}
