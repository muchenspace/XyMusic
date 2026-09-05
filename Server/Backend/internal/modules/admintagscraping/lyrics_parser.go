package admintagscraping

import (
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"
)

type lyricDocument struct {
	Lines         []lyricLine
	HasWordTiming bool
	OffsetMS      int
}

type lyricLine struct {
	StartMS int
	EndMS   int
	Words   []lyricWord
}

type lyricWord struct {
	StartMS int
	EndMS   int
	Text    string
	Timed   bool
}

var (
	offsetPattern     = regexp.MustCompile(`(?i)^\s*\[offset:\s*([+-]?\d+)\s*\]\s*$`)
	lyricLinePattern  = regexp.MustCompile(`^\[(\d+),(\d+)\](.*)$`)
	qrcWordPattern    = regexp.MustCompile(`\((\d+),(\d+)\)`)
	qrcContentPattern = regexp.MustCompile(`(?s)<Lyric_1\s+LyricType="1"\s+LyricContent="(.*?)"\s*/>`)
	krcWordPattern    = regexp.MustCompile(`<(\d+),(\d+),\d+>`)
	yrcWordPattern    = regexp.MustCompile(`\((\d+),(\d+),\d+\)`)
)

func parseQRC(content string) (lyricDocument, error) {
	match := qrcContentPattern.FindStringSubmatch(content)
	if len(match) != 2 {
		return lyricDocument{}, fmt.Errorf("QRC lyric wrapper is missing")
	}
	return parseTimedLines(html.UnescapeString(match[1]), func(line lyricLine, raw string) (lyricLine, bool, error) {
		return parseQRCWords(line, raw)
	})
}

func parseKRC(content string) (lyricDocument, error) {
	return parseTimedLines(content, func(line lyricLine, raw string) (lyricLine, bool, error) {
		return parseKRCWords(line, raw)
	})
}

func parseYRC(content string) (lyricDocument, error) {
	return parseTimedLines(content, func(line lyricLine, raw string) (lyricLine, bool, error) {
		return parseYRCWords(line, raw)
	})
}


func parseTimedLines(content string, parseWords func(lyricLine, string) (lyricLine, bool, error)) (lyricDocument, error) {
	document := lyricDocument{Lines: make([]lyricLine, 0)}
	for _, rawLine := range strings.Split(strings.ReplaceAll(content, "\r", ""), "\n") {
		trimmed := strings.TrimSpace(rawLine)
		if offsetMatch := offsetPattern.FindStringSubmatch(trimmed); len(offsetMatch) == 2 {
			if offset, err := strconv.Atoi(offsetMatch[1]); err == nil {
				document.OffsetMS = offset
			}
			continue
		}
		lineMatch := lyricLinePattern.FindStringSubmatch(trimmed)
		if len(lineMatch) != 4 {
			continue
		}
		start, err := parseInt(lineMatch[1])
		if err != nil {
			return lyricDocument{}, fmt.Errorf("parse lyric line start: %w", err)
		}
		duration, err := parseInt(lineMatch[2])
		if err != nil {
			return lyricDocument{}, fmt.Errorf("parse lyric line duration: %w", err)
		}
		line, hasWordTiming, err := parseWords(lyricLine{StartMS: start, EndMS: start + duration}, lineMatch[3])
		if err != nil {
			return lyricDocument{}, err
		}
		if len(line.Words) == 0 {
			continue
		}
		document.Lines = append(document.Lines, line)
		document.HasWordTiming = document.HasWordTiming || hasWordTiming
	}
	if len(document.Lines) == 0 {
		return lyricDocument{}, fmt.Errorf("lyric content has no timed lines")
	}
	return document, nil
}

func parseQRCWords(line lyricLine, raw string) (lyricLine, bool, error) {
	if strings.HasPrefix(raw, "[") {
		if closing := strings.Index(raw, "]"); closing >= 0 {
			raw = raw[closing+1:]
		}
	}
	markers := qrcWordPattern.FindAllStringSubmatchIndex(raw, -1)
	if len(markers) == 0 {
		text := strings.TrimSpace(raw)
		if text == "" {
			return line, false, nil
		}
		line.Words = []lyricWord{{StartMS: line.StartMS, EndMS: line.EndMS, Text: text}}
		return line, false, nil
	}
	words := make([]lyricWord, 0, len(markers))
	for index, marker := range markers {
		textStart := 0
		if index > 0 {
			textStart = markers[index-1][1]
		}
		text := raw[textStart:marker[0]]
		start, err := parseInt(raw[marker[2]:marker[3]])
		if err != nil {
			return lyricLine{}, false, err
		}
		duration, err := parseInt(raw[marker[4]:marker[5]])
		if err != nil {
			return lyricLine{}, false, err
		}
		if text == "" {
			continue
		}
		words = append(words, lyricWord{StartMS: start, EndMS: start + duration, Text: text, Timed: true})
	}
	if len(words) == 0 {
		return line, false, nil
	}
	line.Words = words
	return line, true, nil
}

func parseKRCWords(line lyricLine, raw string) (lyricLine, bool, error) {
	if strings.HasPrefix(raw, "[") {
		if closing := strings.Index(raw, "]"); closing >= 0 {
			raw = raw[closing+1:]
		}
	}
	markers := krcWordPattern.FindAllStringSubmatchIndex(raw, -1)
	if len(markers) == 0 {
		text := strings.TrimSpace(raw)
		if text == "" {
			return line, false, nil
		}
		line.Words = []lyricWord{{StartMS: line.StartMS, EndMS: line.EndMS, Text: text}}
		return line, false, nil
	}
	words := make([]lyricWord, 0, len(markers))
	for index, marker := range markers {
		relStart, err := parseInt(raw[marker[2]:marker[3]])
		if err != nil {
			return lyricLine{}, false, err
		}
		duration, err := parseInt(raw[marker[4]:marker[5]])
		if err != nil {
			return lyricLine{}, false, err
		}
		textEnd := len(raw)
		if index+1 < len(markers) {
			textEnd = markers[index+1][0]
		}
		text := raw[marker[1]:textEnd]
		if text == "" {
			continue
		}
		startMS := line.StartMS + relStart
		words = append(words, lyricWord{
			StartMS: startMS,
			EndMS:   startMS + duration,
			Text:    text,
			Timed:   true,
		})
	}
	if len(words) == 0 {
		return line, false, nil
	}
	line.Words = words
	return line, true, nil
}

func parseYRCWords(line lyricLine, raw string) (lyricLine, bool, error) {
	if strings.HasPrefix(raw, "[") {
		if closing := strings.Index(raw, "]"); closing >= 0 {
			raw = raw[closing+1:]
		}
	}
	markers := yrcWordPattern.FindAllStringSubmatchIndex(raw, -1)
	if len(markers) == 0 {
		text := strings.TrimSpace(raw)
		if text == "" {
			return line, false, nil
		}
		line.Words = []lyricWord{{StartMS: line.StartMS, EndMS: line.EndMS, Text: text}}
		return line, false, nil
	}
	words := make([]lyricWord, 0, len(markers))
	for index, marker := range markers {
		start, err := parseInt(raw[marker[2]:marker[3]])
		if err != nil {
			return lyricLine{}, false, err
		}
		duration, err := parseInt(raw[marker[4]:marker[5]])
		if err != nil {
			return lyricLine{}, false, err
		}
		textEnd := len(raw)
		if index+1 < len(markers) {
			textEnd = markers[index+1][0]
		}
		text := raw[marker[1]:textEnd]
		if text == "" {
			continue
		}
		words = append(words, lyricWord{
			StartMS: start,
			EndMS:   start + duration,
			Text:    text,
			Timed:   true,
		})
	}
	if len(words) == 0 {
		return line, false, nil
	}
	line.Words = words
	return line, true, nil
}

func renderEnhancedLRC(document lyricDocument) string {
	lines := make([]string, 0, len(document.Lines)+1)
	if document.OffsetMS != 0 {
		lines = append(lines, fmt.Sprintf("[offset:%d]", document.OffsetMS))
	}
	for _, line := range document.Lines {
		text := strings.Builder{}
		text.WriteString(formatLyricTimestamp(line.StartMS, '[', ']'))
		for _, word := range line.Words {
			if word.Timed || document.HasWordTiming {
				text.WriteString(formatLyricTimestamp(word.StartMS, '<', '>'))
			}
			text.WriteString(word.Text)
		}
		if document.HasWordTiming && len(line.Words) > 0 {
			// Enhanced LRC has no standard end marker; preserve QRC's real final end time.
			text.WriteString(formatLyricTimestamp(line.Words[len(line.Words)-1].EndMS, '<', '>'))
		}
		if text.Len() > len(formatLyricTimestamp(line.StartMS, '[', ']')) {
			lines = append(lines, text.String())
		}
	}
	return strings.Join(lines, "\n")
}

func formatLyricTimestamp(milliseconds int, opening, closing byte) string {
	if milliseconds < 0 {
		milliseconds = 0
	}
	minutes := milliseconds / 60_000
	seconds := milliseconds % 60_000 / 1_000
	fraction := milliseconds % 1_000
	return fmt.Sprintf("%c%02d:%02d.%03d%c", opening, minutes, seconds, fraction, closing)
}

func parseInt(value string) (int, error) {
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 0 {
		if err == nil {
			err = fmt.Errorf("value must be non-negative")
		}
		return 0, err
	}
	return parsed, nil
}
