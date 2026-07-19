package adminsources

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/text/unicode/norm"
)

type cueTrack struct {
	Number    int
	Title     string
	Performer string
	File      string
	StartMS   int
	EndMS     *int
}

type cueSheet struct {
	Title      string
	Performer  string
	Date       string
	DiscNumber *int
	Tracks     []cueTrack
}

type mutableCueTrack struct {
	Number    int
	Title     string
	Performer string
	File      string
	StartMS   *int
}

func parseCueSheet(content string) (cueSheet, error) {
	lines := strings.Split(strings.TrimPrefix(content, "\ufeff"), "\n")
	var result cueSheet
	currentFile := ""
	var current *mutableCueTrack
	tracks := make([]*mutableCueTrack, 0)
	for _, raw := range lines {
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		command := strings.ToUpper(fields[0])
		value := strings.TrimSpace(strings.TrimPrefix(line, fields[0]))
		switch command {
		case "FILE":
			match := cueFilePattern.FindStringSubmatch(value)
			if len(match) == 0 {
				return cueSheet{}, errors.New("CUE FILE entry is invalid")
			}
			currentFile = cleanCueValue(firstNonEmptyString(match[1], match[2]))
			if currentFile == "" {
				return cueSheet{}, errors.New("CUE FILE entry is invalid")
			}
		case "TRACK":
			if currentFile == "" {
				return cueSheet{}, errors.New("CUE TRACK appears before FILE")
			}
			match := cueTrackPattern.FindStringSubmatch(value)
			if len(match) == 0 {
				continue
			}
			number, _ := strconv.Atoi(match[1])
			current = &mutableCueTrack{Number: number, File: currentFile}
			tracks = append(tracks, current)
		case "TITLE":
			if current != nil {
				current.Title = quotedCueValue(value)
			} else {
				result.Title = quotedCueValue(value)
			}
		case "PERFORMER":
			if current != nil {
				current.Performer = quotedCueValue(value)
			} else {
				result.Performer = quotedCueValue(value)
			}
		case "INDEX":
			if current == nil {
				continue
			}
			match := cueIndexPattern.FindStringSubmatch(value)
			if len(match) == 0 {
				continue
			}
			milliseconds, err := cueTimeMilliseconds(match[1], match[2], match[3])
			if err != nil {
				return cueSheet{}, err
			}
			current.StartMS = &milliseconds
		case "REM":
			match := cueREM.FindStringSubmatch(value)
			if len(match) == 0 {
				continue
			}
			key := strings.ToUpper(match[1])
			remValue := quotedCueValue(match[2])
			if (key == "DATE" || key == "YEAR") && cueDatePattern.MatchString(remValue) {
				result.Date = remValue
			}
			if key == "DISCNUMBER" {
				parsed, err := strconv.Atoi(remValue)
				if err == nil && parsed > 0 && parsed <= 999 {
					result.DiscNumber = &parsed
				}
			}
		}
	}
	audioTracks := make([]*mutableCueTrack, 0, len(tracks))
	for _, track := range tracks {
		if track.StartMS != nil {
			audioTracks = append(audioTracks, track)
		}
	}
	if len(audioTracks) == 0 {
		return cueSheet{}, errors.New("CUE contains no playable AUDIO tracks")
	}
	seenNumbers := make(map[int]struct{}, len(audioTracks))
	for index, track := range audioTracks {
		if _, duplicate := seenNumbers[track.Number]; duplicate {
			return cueSheet{}, errors.New("CUE track numbers must be unique")
		}
		seenNumbers[track.Number] = struct{}{}
		var end *int
		for next := index + 1; next < len(audioTracks); next++ {
			if audioTracks[next].File == track.File {
				value := *audioTracks[next].StartMS
				end = &value
				break
			}
		}
		if end != nil && *end <= *track.StartMS {
			return cueSheet{}, errors.New("CUE track indexes must increase within each file")
		}
		result.Tracks = append(result.Tracks, cueTrack{
			Number: track.Number, Title: track.Title, Performer: track.Performer,
			File: track.File, StartMS: *track.StartMS, EndMS: end,
		})
	}
	return result, nil
}

func cueTimeMilliseconds(minutes, seconds, frames string) (int, error) {
	minuteValue, _ := strconv.Atoi(minutes)
	secondValue, _ := strconv.Atoi(seconds)
	frameValue, _ := strconv.Atoi(frames)
	if secondValue > 59 || frameValue > 74 {
		return 0, errors.New("CUE INDEX time is invalid")
	}
	return int(float64(minuteValue*60+secondValue)*1000 + float64(frameValue)/75*1000 + 0.5), nil
}

func quotedCueValue(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
		value = value[1 : len(value)-1]
	}
	return cleanCueValue(value)
}

func cleanCueValue(value string) string { return strings.TrimSpace(norm.NFKC.String(value)) }

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func cueTrackTitle(track cueTrack) string {
	if track.Title != "" {
		return track.Title
	}
	return fmt.Sprintf("Track %d", track.Number)
}

var (
	cueFilePattern  = regexp.MustCompile(`^"([^"]+)"(?:\s+\S+)?$|^(\S+)(?:\s+\S+)?$`)
	cueTrackPattern = regexp.MustCompile(`(?i)^(\d{1,3})\s+AUDIO$`)
	cueIndexPattern = regexp.MustCompile(`^01\s+(\d{1,3}):(\d{2}):(\d{2})$`)
	cueREM          = regexp.MustCompile(`(?i)^(DATE|YEAR|DISCNUMBER)\s+(.+)$`)
	cueDatePattern  = regexp.MustCompile(`^\d{4}(?:-\d{2}(?:-\d{2})?)?$`)
)
