package lyrics

import (
	"fmt"
	"regexp"
	"strings"
)

type Timing string

const (
	TimingLine Timing = "LINE"
	TimingWord Timing = "WORD"
)

func ValidTiming(value string) bool {
	return value == string(TimingLine) || value == string(TimingWord)
}

var (
	wordTimestampPattern       = regexp.MustCompile(`<([0-9]{1,3}):([0-5][0-9])(?:[.:]([0-9]{1,3}))?>`)
	wordTimestampStartPattern  = regexp.MustCompile(`^\s*<[0-9]{1,3}:[0-5][0-9](?:[.:][0-9]{1,3})?>`)
	anyWordMarkerPattern       = regexp.MustCompile(`<[^>]*(?:>|$)`)
	lineTimestampPrefixPattern = regexp.MustCompile(
		`^\s*(?:\[[0-9]{1,3}:[0-5][0-9](?:[.:][0-9]{1,3})?\])+\s*`,
	)
	metadataOnlyLinePattern = regexp.MustCompile(
		`^\s*(?:\[[A-Za-z][A-Za-z0-9_-]*:[^\[\]\r\n]*\]\s*)+$`,
	)
)

func DetectTiming(format, content string) Timing {
	if format == "LRC" && completeWordTimedLRC(content) {
		return TimingWord
	}
	return TimingLine
}

func ValidateDocument(format string, timing Timing, content string) error {
	if format != "LRC" && format != "PLAIN" {
		return fmt.Errorf("lyrics format %q is invalid", format)
	}
	if timing != TimingLine && timing != TimingWord {
		return fmt.Errorf("lyrics timing %q is invalid", timing)
	}
	detected := DetectTiming(format, content)
	if timing != detected {
		return fmt.Errorf("lyrics timing %q does not match content timing %q", timing, detected)
	}
	return nil
}

func completeWordTimedLRC(content string) bool {
	hasTimedLyricLine := false
	for _, rawLine := range strings.Split(strings.ReplaceAll(content, "\r", ""), "\n") {
		if strings.TrimSpace(rawLine) == "" {
			continue
		}
		prefix := lineTimestampPrefixPattern.FindStringIndex(rawLine)
		if prefix == nil {
			if metadataOnlyLinePattern.MatchString(rawLine) {
				continue
			}
			return false
		}
		body := strings.TrimSpace(rawLine[prefix[1]:])
		if body == "" {
			continue
		}
		hasTimedLyricLine = true
		remaining := wordTimestampPattern.ReplaceAllString(body, "")
		if !wordTimestampStartPattern.MatchString(body) || anyWordMarkerPattern.MatchString(remaining) ||
			strings.TrimSpace(remaining) == "" || !wordTimestampsAreNondecreasing(body) {
			return false
		}
	}
	return hasTimedLyricLine
}

func wordTimestampsAreNondecreasing(body string) bool {
	previous := -1
	for _, parts := range wordTimestampPattern.FindAllStringSubmatch(body, -1) {
		fraction := decimalDigits(parts[3])
		for digits := len(parts[3]); digits < 3; digits++ {
			fraction *= 10
		}
		current := decimalDigits(parts[1])*60_000 + decimalDigits(parts[2])*1_000 + fraction
		if current < previous {
			return false
		}
		previous = current
	}
	return true
}

func decimalDigits(value string) int {
	result := 0
	for index := range value {
		result = result*10 + int(value[index]-'0')
	}
	return result
}
