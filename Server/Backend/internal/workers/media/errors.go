package media

import (
	"regexp"
	"time"
)

var (
	workerURLPattern    = regexp.MustCompile(`https?://\S+`)
	workerSecretPattern = regexp.MustCompile(`[A-Za-z0-9_-]{48,}`)
)

func safeWorkerError(err error) string {
	message := "Unknown worker failure"
	if err != nil {
		message = err.Error()
	}
	message = truncateRunes(message, 2_000)
	message = workerURLPattern.ReplaceAllString(message, "[REDACTED_URL]")
	return workerSecretPattern.ReplaceAllString(message, "[REDACTED]")
}

func retryDelay(attempts int) time.Duration {
	if attempts < 0 {
		attempts = 0
	}
	if attempts > 10 {
		attempts = 10
	}
	delay := time.Duration(1<<attempts) * 5 * time.Second
	if delay > time.Hour {
		return time.Hour
	}
	return delay
}

func lastRunes(value string, maximum int) string {
	runes := []rune(value)
	if len(runes) <= maximum {
		return string(runes)
	}
	return string(runes[len(runes)-maximum:])
}
