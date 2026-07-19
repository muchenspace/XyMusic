package media

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"time"

	"xymusic/server/internal/platform/processio"
)

type OSProcessRunner struct{}

func (OSProcessRunner) Run(
	ctx context.Context,
	executable string,
	arguments []string,
	timeout time.Duration,
) (ProcessResult, error) {
	processContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	command := exec.CommandContext(processContext, executable, arguments...)
	command.Cancel = func() error { return requestProcessTermination(command.Process) }
	command.Env = mediaProcessEnvironment(os.Environ())
	command.WaitDelay = 5 * time.Second
	stdout := processio.NewHeadBuffer(maxProcessStdoutBytes)
	stderr := processio.NewTailBuffer(maxProcessStderrBytes)
	command.Stdout = stdout
	command.Stderr = stderr
	err := command.Run()
	result := ProcessResult{
		Stdout: stdout.String(), Stderr: stderr.String(),
		StdoutTruncated: stdout.Truncated(),
	}
	if errors.Is(ctx.Err(), context.Canceled) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		if cause := context.Cause(ctx); cause != nil {
			return ProcessResult{}, cause
		}
		return ProcessResult{}, ctx.Err()
	}
	if errors.Is(processContext.Err(), context.DeadlineExceeded) {
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

func mediaProcessEnvironment(source []string) []string {
	allowed := map[string]struct{}{
		"PATH": {}, "PATHEXT": {}, "SYSTEMROOT": {}, "WINDIR": {}, "COMSPEC": {},
		"TEMP": {}, "TMP": {}, "TMPDIR": {}, "HOME": {}, "USERPROFILE": {},
		"LANG": {}, "LC_ALL": {}, "LC_CTYPE": {}, "LD_LIBRARY_PATH": {}, "DYLD_LIBRARY_PATH": {},
	}
	values := make(map[string]string, len(source))
	for _, entry := range source {
		key, value, found := strings.Cut(entry, "=")
		if !found {
			continue
		}
		upper := strings.ToUpper(key)
		if _, found := allowed[upper]; found {
			values[upper] = value
		}
	}
	keys := []string{
		"PATH", "PATHEXT", "SYSTEMROOT", "WINDIR", "COMSPEC", "TEMP", "TMP", "TMPDIR",
		"HOME", "USERPROFILE", "LANG", "LC_ALL", "LC_CTYPE", "LD_LIBRARY_PATH", "DYLD_LIBRARY_PATH",
	}
	result := make([]string, 0, len(values))
	for _, key := range keys {
		if value, found := values[key]; found {
			result = append(result, key+"="+value)
		}
	}
	return result
}
