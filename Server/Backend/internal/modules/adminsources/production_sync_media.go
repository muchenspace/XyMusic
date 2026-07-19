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

	"golang.org/x/text/unicode/norm"

	"xymusic/server/internal/modules/adminmetadata"
)

func readSidecarLyrics(audioPath string) ([]scannedLyric, error) {
	directory := filepath.Dir(audioPath)
	stem := normalizePlatformPath(strings.TrimSuffix(filepath.Base(audioPath), filepath.Ext(audioPath)))
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, err
	}
	type candidate struct {
		path, language, format string
		base                   bool
	}
	candidates := make([]candidate, 0)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		extension := strings.ToLower(filepath.Ext(entry.Name()))
		if extension != ".lrc" && extension != ".txt" {
			continue
		}
		rawStem := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
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
			path: filepath.Join(directory, entry.Name()), language: language, format: format, base: base,
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
		metadata, err := os.Stat(candidate.path)
		if err != nil {
			return nil, err
		}
		if metadata.Size() > 1_000_000 {
			continue
		}
		content, err := os.ReadFile(candidate.path)
		if err != nil {
			return nil, err
		}
		value := strings.TrimSpace(norm.NFC.String(strings.TrimPrefix(string(content), "\ufeff")))
		if value == "" {
			continue
		}
		seen[candidate.language] = struct{}{}
		result = append(result, scannedLyric{
			Content: value, Format: candidate.format, Language: candidate.language,
			Origin: "EXTERNAL", IsDefault: len(result) == 0,
		})
	}
	return result, nil
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
	if _, err := io.Copy(hasher, file); err != nil {
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
