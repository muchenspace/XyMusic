package adminsources

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"golang.org/x/text/unicode/norm"

	"xymusic/server/internal/modules/adminmetadata"
	sharedlyrics "xymusic/server/internal/shared/lyrics"
)

func readSidecarLyrics(audioPath string) ([]scannedLyric, error) {
	return readSidecarLyricsCached(nil, audioPath)
}

func readSidecarLyricsCached(snapshot *sourceScanSnapshot, audioPath string) ([]scannedLyric, error) {
	directory := filepath.Dir(audioPath)
	stem := normalizePlatformPath(strings.TrimSuffix(filepath.Base(audioPath), filepath.Ext(audioPath)))
	var names []string
	if snapshot != nil {
		var err error
		names, err = snapshot.sidecarNamesForStem(directory, stem)
		if err != nil {
			return nil, err
		}
	} else {
		entries, err := os.ReadDir(directory)
		if err != nil {
			return nil, err
		}
		names = make([]string, 0, len(entries))
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			names = append(names, entry.Name())
		}
	}
	type candidate struct {
		path, language, format string
		base                   bool
	}
	candidates := make([]candidate, 0)
	for _, name := range names {
		extension := strings.ToLower(filepath.Ext(name))
		if extension != ".lrc" && extension != ".txt" {
			continue
		}
		rawStem := strings.TrimSuffix(name, filepath.Ext(name))
		candidateStem := normalizePlatformPath(rawStem)
		language := "und"
		base := candidateStem == stem
		if !base {
			separator := strings.LastIndex(rawStem, ".")
			if separator < 1 || normalizePlatformPath(rawStem[:separator]) != stem {
				continue
			}
			language = normalizeLyricLanguage(rawStem[separator+1:])
		}
		format := "PLAIN"
		if extension == ".lrc" {
			format = "LRC"
		}
		candidates = append(candidates, candidate{
			path: filepath.Join(directory, name), language: language, format: format, base: base,
		})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].base != candidates[j].base {
			return candidates[i].base
		}
		if candidates[i].language != candidates[j].language {
			return candidates[i].language < candidates[j].language
		}
		if candidates[i].format != candidates[j].format {
			return candidates[i].format == "LRC"
		}
		return candidates[i].path < candidates[j].path
	})
	seen := make(map[string]struct{})
	result := make([]scannedLyric, 0)
	for _, candidate := range candidates {
		if _, exists := seen[candidate.language]; exists {
			continue
		}
		content, err := readSidecarContent(candidate.path)
		if err != nil {
			return nil, err
		}
		if content == nil {
			continue
		}
		value := strings.TrimSpace(norm.NFC.String(strings.TrimPrefix(string(content), "\ufeff")))
		if value == "" {
			continue
		}
		seen[candidate.language] = struct{}{}
		result = append(result, scannedLyric{
			Content: value, Format: candidate.format, Language: candidate.language,
			Timing: sharedlyrics.DetectTiming(candidate.format, value),
			Origin: "EXTERNAL", IsDefault: len(result) == 0,
		})
	}
	return result, nil
}

func readSidecarContent(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	metadata, statErr := file.Stat()
	if statErr != nil {
		_ = file.Close()
		return nil, statErr
	}
	if metadata.Size() > 1_000_000 {
		_ = file.Close()
		return nil, nil
	}
	content, readErr := io.ReadAll(io.LimitReader(file, 1_000_001))
	closeErr := file.Close()
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if len(content) > 1_000_000 {
		return nil, nil
	}
	return content, nil
}

func mergeLyrics(sidecars []scannedLyric, embedded *adminmetadata.MetadataLyrics) []scannedLyric {
	result := append([]scannedLyric(nil), sidecars...)
	languages := make(map[string]struct{}, len(result))
	for _, lyric := range result {
		languages[lyric.Language] = struct{}{}
	}
	if embedded != nil {
		language := normalizeLyricLanguage(embedded.Language)
		if _, exists := languages[language]; !exists {
			result = append(result, scannedLyric{
				Content: embedded.Content, Format: embedded.Format, Language: language,
				Timing: embedded.Timing,
				Origin: "SCAN", IsDefault: len(result) == 0,
			})
		}
	}
	return result
}

func normalizeLyricLanguage(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if lyricLanguagePattern.MatchString(value) {
		return value
	}
	return "und"
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hasher := sha256.New()
	buffer := sourceHashBufferPool.Get().([]byte)
	defer sourceHashBufferPool.Put(buffer)
	if _, err := io.CopyBuffer(hasher, file, buffer); err != nil {
		return "", err
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func sourceMediaType(path string) (string, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".flac":
		return "audio/flac", nil
	case ".aac":
		return "audio/aac", nil
	case ".aif", ".aiff":
		return "audio/aiff", nil
	case ".ape":
		return "audio/ape", nil
	case ".caf":
		return "audio/x-caf", nil
	case ".mp3":
		return "audio/mpeg", nil
	case ".m4a", ".mp4":
		return "audio/mp4", nil
	case ".mka", ".webm":
		return "audio/webm", nil
	case ".ogg", ".opus":
		return "audio/ogg", nil
	case ".wav":
		return "audio/wav", nil
	case ".wma":
		return "audio/x-ms-wma", nil
	default:
		return "", errors.New("unsupported audio file extension")
	}
}

var lyricLanguagePattern = regexp.MustCompile(`^[a-z]{2,8}(?:-[a-z0-9]{2,8})*$`)

var sourceHashBufferPool = sync.Pool{
	New: func() any { return make([]byte, 64*1024) },
}
