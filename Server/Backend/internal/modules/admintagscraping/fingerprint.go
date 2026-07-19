package admintagscraping

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"xymusic/server/internal/platform/processio"
	"xymusic/server/internal/shared/apperror"
)

const (
	maximumFingerprintStdout = 4 * 1024 * 1024
	maximumFingerprintStderr = 256 * 1024
)

type FPcalcFingerprinter struct {
	executable string
	timeout    time.Duration
}

var _ Fingerprinter = (*FPcalcFingerprinter)(nil)

func NewFPcalcFingerprinter(executable string) (*FPcalcFingerprinter, error) {
	executable = strings.TrimSpace(executable)
	if executable == "" {
		return nil, errors.New("fpcalc executable path is required")
	}
	return &FPcalcFingerprinter{executable: executable, timeout: 45 * time.Second}, nil
}

// ConfiguredFingerprinter converts optional configuration into the service
// port. A blank path deliberately returns nil so the service can fail before
// querying the library database.
func ConfiguredFingerprinter(executable string) Fingerprinter {
	if strings.TrimSpace(executable) == "" {
		return nil
	}
	return &FPcalcFingerprinter{executable: strings.TrimSpace(executable), timeout: 45 * time.Second}
}

func (fingerprinter *FPcalcFingerprinter) Fingerprint(
	ctx context.Context,
	path string,
	startMS int,
	endMS *int,
) (FingerprintResult, error) {
	if fingerprinter == nil || strings.TrimSpace(fingerprinter.executable) == "" {
		return FingerprintResult{}, apperror.DependencyUnavailable("fpcalc is not configured")
	}
	timeout := fingerprinter.timeout
	if timeout <= 0 {
		timeout = 45 * time.Second
	}
	commandContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	lengthSeconds := 120
	if endMS != nil {
		lengthSeconds = int(math.Ceil(float64(*endMS-startMS) / 1_000))
		lengthSeconds = max(1, min(120, lengthSeconds))
	}
	arguments := make([]string, 0, 6)
	if startMS > 0 {
		arguments = append(arguments, "-offset", strconv.Itoa(startMS/1_000))
	}
	arguments = append(arguments, "-length", strconv.Itoa(lengthSeconds), path)
	command := exec.CommandContext(commandContext, fingerprinter.executable, arguments...)
	command.WaitDelay = 5 * time.Second
	stdout := processio.NewHeadBuffer(maximumFingerprintStdout)
	stderr := processio.NewTailBuffer(maximumFingerprintStderr)
	command.Stdout = stdout
	command.Stderr = stderr
	err := command.Run()
	if err != nil {
		if ctx.Err() != nil {
			return FingerprintResult{}, ctx.Err()
		}
		if errors.Is(commandContext.Err(), context.DeadlineExceeded) {
			return FingerprintResult{}, apperror.DependencyUnavailable("fpcalc fingerprint calculation timed out")
		}
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			detail := strings.TrimSpace(stderr.String())
			if detail == "" {
				detail = "fpcalc execution failed"
			}
			if len(detail) > 500 {
				detail = detail[:500]
			}
			return FingerprintResult{}, apperror.DependencyUnavailable(detail)
		}
		return FingerprintResult{}, apperror.DependencyUnavailable("fpcalc could not be started")
	}
	if stdout.Truncated() {
		return FingerprintResult{}, apperror.DependencyUnavailable("fpcalc returned an unexpectedly large response")
	}
	durationMatch := durationPattern.FindStringSubmatch(stdout.String())
	fingerprintMatch := fingerprintPattern.FindStringSubmatch(stdout.String())
	if len(durationMatch) != 2 || len(fingerprintMatch) != 2 {
		return FingerprintResult{}, apperror.DependencyUnavailable("fpcalc did not return a valid fingerprint")
	}
	duration, err := strconv.ParseFloat(strings.TrimSpace(durationMatch[1]), 64)
	if err != nil || duration <= 0 || math.IsNaN(duration) || math.IsInf(duration, 0) {
		return FingerprintResult{}, apperror.DependencyUnavailable("fpcalc returned an invalid duration")
	}
	fingerprint := strings.TrimSpace(fingerprintMatch[1])
	if fingerprint == "" {
		return FingerprintResult{}, apperror.DependencyUnavailable("fpcalc returned an empty fingerprint")
	}
	return FingerprintResult{DurationSeconds: duration, Fingerprint: fingerprint}, nil
}

func (fingerprinter *FPcalcFingerprinter) String() string {
	if fingerprinter == nil {
		return "fpcalc(disabled)"
	}
	return fmt.Sprintf("fpcalc(%s)", fingerprinter.executable)
}

var (
	durationPattern    = regexp.MustCompile(`(?m)^DURATION=(.+)$`)
	fingerprintPattern = regexp.MustCompile(`(?m)^FINGERPRINT=(.+)$`)
)
