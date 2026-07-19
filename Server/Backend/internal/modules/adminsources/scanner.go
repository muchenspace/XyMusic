package adminsources

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/text/unicode/norm"
)

var supportedAudioExtensions = map[string]struct{}{
	".aac": {}, ".aif": {}, ".aiff": {}, ".ape": {}, ".caf": {}, ".flac": {},
	".m4a": {}, ".mka": {}, ".mp3": {}, ".mp4": {}, ".ogg": {}, ".opus": {},
	".wav": {}, ".webm": {}, ".wma": {},
}

type DiscoveredFile struct {
	AudioPath    string
	RelativePath string
	CuePath      string
	ScanError    error
}

type FileSynchronizer interface {
	ProcessFile(context.Context, string, string, DiscoveredFile, time.Time) error
	ArchiveMissing(context.Context, string, time.Time, time.Time) (int, error)
}

type FilesystemScanner struct {
	synchronizer FileSynchronizer
	now          func() time.Time
}

func NewFilesystemScanner(synchronizer FileSynchronizer) (*FilesystemScanner, error) {
	if synchronizer == nil {
		return nil, errors.New("local library file synchronizer is required")
	}
	return &FilesystemScanner{synchronizer: synchronizer, now: func() time.Time { return time.Now().UTC() }}, nil
}

func (scanner *FilesystemScanner) Scan(ctx context.Context, input ScanInput) (ScanResult, error) {
	metadata, err := os.Stat(input.Directory)
	if err != nil || !metadata.IsDir() {
		if err == nil {
			err = errors.New("configured path is not a directory")
		}
		return ScanResult{}, err
	}
	include, err := compilePatterns(input.IncludePatterns)
	if err != nil {
		return ScanResult{}, err
	}
	exclude, err := compilePatterns(input.ExcludePatterns)
	if err != nil {
		return ScanResult{}, err
	}
	startedAt := scanner.now()
	progress := ScanProgress{}
	if input.OnProgress != nil {
		if err := input.OnProgress(ctx, progress); err != nil {
			return ScanResult{}, err
		}
	}
	files, err := discoverLibraryFiles(input.Directory, include, exclude)
	if err != nil {
		return ScanResult{}, err
	}
	progress.DiscoveredFiles = len(files)
	for start := 0; start < len(files); start += 4 {
		if cancelled, err := scanCancelled(ctx, input.IsCancelled); err != nil {
			return ScanResult{}, err
		} else if cancelled {
			return ScanResult{}, ErrScanCancelled
		}
		end := min(start+4, len(files))
		batch := files[start:end]
		errorsByIndex := make([]error, len(batch))
		var group sync.WaitGroup
		for index, file := range batch {
			index, file := index, file
			group.Add(1)
			go func() {
				defer group.Done()
				if file.ScanError != nil {
					recordErr := scanner.synchronizer.ProcessFile(ctx, input.RootID, input.ScanRunID, file, startedAt)
					errorsByIndex[index] = errors.Join(file.ScanError, recordErr)
					return
				}
				if cancelled, err := scanCancelled(ctx, input.IsCancelled); err != nil {
					errorsByIndex[index] = err
				} else if cancelled {
					errorsByIndex[index] = ErrScanCancelled
				} else {
					errorsByIndex[index] = scanner.synchronizer.ProcessFile(ctx, input.RootID, input.ScanRunID, file, startedAt)
				}
			}()
		}
		group.Wait()
		for _, processErr := range errorsByIndex {
			if errors.Is(processErr, ErrScanCancelled) || errors.Is(processErr, context.Canceled) {
				return ScanResult{}, ErrScanCancelled
			}
			progress.ProcessedFiles++
			if processErr != nil {
				progress.FailedFiles++
			}
			if input.OnProgress != nil {
				if err := input.OnProgress(ctx, progress); err != nil {
					return ScanResult{}, err
				}
			}
		}
	}
	if cancelled, err := scanCancelled(ctx, input.IsCancelled); err != nil {
		return ScanResult{}, err
	} else if cancelled {
		return ScanResult{}, ErrScanCancelled
	}
	archived, err := scanner.synchronizer.ArchiveMissing(ctx, input.RootID, startedAt, scanner.now())
	if err != nil {
		return ScanResult{}, err
	}
	return ScanResult{
		DiscoveredFiles: progress.DiscoveredFiles, ProcessedFiles: progress.ProcessedFiles,
		FailedFiles: progress.FailedFiles, ArchivedFiles: archived,
	}, nil
}

func discoverLibraryFiles(root string, include, exclude []*regexp.Regexp) ([]DiscoveredFile, error) {
	directories := make([]string, 0)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			directories = append(directories, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	files := make([]DiscoveredFile, 0)
	cueOwned := make(map[string]string)
	for _, directory := range directories {
		entries, err := os.ReadDir(directory)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if entry.IsDir() || strings.ToLower(filepath.Ext(entry.Name())) != ".cue" {
				continue
			}
			cuePath := filepath.Join(directory, entry.Name())
			references, parseErr := cueReferences(cuePath)
			if parseErr != nil {
				files = append(files, DiscoveredFile{AudioPath: cuePath, RelativePath: relativeLibraryPath(root, cuePath), CuePath: cuePath, ScanError: parseErr})
				continue
			}
			for _, reference := range references {
				target, resolveErr := resolveFileWithinRoot(root, filepath.Join(directory, reference))
				if resolveErr == nil {
					if _, supported := supportedAudioExtensions[strings.ToLower(filepath.Ext(target))]; !supported {
						resolveErr = errors.New("CUE referenced an unsupported audio container")
					}
				}
				if resolveErr != nil {
					files = append(files, DiscoveredFile{AudioPath: cuePath, RelativePath: relativeLibraryPath(root, cuePath), CuePath: cuePath, ScanError: resolveErr})
					continue
				}
				relative := normalizedRelativeLibraryPath(root, target)
				if !matchesPatterns(relative, include, exclude) {
					continue
				}
				normalizedTarget := normalizePlatformPath(target)
				if previous, exists := cueOwned[normalizedTarget]; exists && previous != cuePath {
					files = append(files, DiscoveredFile{AudioPath: cuePath, RelativePath: relativeLibraryPath(root, cuePath), CuePath: cuePath, ScanError: errors.New("multiple CUE files reference the same audio source")})
					continue
				}
				cueOwned[normalizedTarget] = cuePath
				files = append(files, DiscoveredFile{AudioPath: target, RelativePath: relativeLibraryPath(root, target), CuePath: cuePath})
			}
		}
	}
	for _, directory := range directories {
		entries, err := os.ReadDir(directory)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			path := filepath.Join(directory, entry.Name())
			if _, supported := supportedAudioExtensions[strings.ToLower(filepath.Ext(path))]; !supported {
				continue
			}
			if _, owned := cueOwned[normalizePlatformPath(path)]; owned {
				continue
			}
			relative := normalizedRelativeLibraryPath(root, path)
			if matchesPatterns(relative, include, exclude) {
				files = append(files, DiscoveredFile{AudioPath: path, RelativePath: relativeLibraryPath(root, path)})
			}
		}
	}
	sort.SliceStable(files, func(i, j int) bool { return files[i].RelativePath < files[j].RelativePath })
	return files, nil
}

func cueReferences(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	quoted := regexp.MustCompile(`(?i)^\s*FILE\s+"([^"]+)"\s+\S+`)
	unquoted := regexp.MustCompile(`(?i)^\s*FILE\s+(.+?)\s+\S+\s*$`)
	seen := make(map[string]struct{})
	result := make([]string, 0)
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimPrefix(scanner.Text(), "\ufeff")
		match := quoted.FindStringSubmatch(line)
		if len(match) == 0 {
			match = unquoted.FindStringSubmatch(line)
		}
		if len(match) < 2 {
			continue
		}
		value := strings.TrimSpace(match[1])
		if value == "" {
			return nil, errors.New("CUE file reference is empty")
		}
		if _, exists := seen[value]; !exists {
			seen[value] = struct{}{}
			result = append(result, value)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(result) == 0 {
		return nil, errors.New("CUE sheet contains no audio files")
	}
	return result, nil
}

func compilePatterns(values []string) ([]*regexp.Regexp, error) {
	patterns := make([]*regexp.Regexp, 0, len(values))
	for _, value := range values {
		pattern, err := compileLibraryGlob(value)
		if err != nil {
			return nil, err
		}
		patterns = append(patterns, pattern)
	}
	return patterns, nil
}

func matchesPatterns(path string, include, exclude []*regexp.Regexp) bool {
	included := len(include) == 0
	for _, pattern := range include {
		if pattern.MatchString(path) {
			included = true
			break
		}
	}
	if !included {
		return false
	}
	for _, pattern := range exclude {
		if pattern.MatchString(path) {
			return false
		}
	}
	return true
}

func scanCancelled(ctx context.Context, callback func(context.Context) (bool, error)) (bool, error) {
	if ctx.Err() != nil {
		return true, nil
	}
	if callback == nil {
		return false, nil
	}
	return callback(ctx)
}

func relativeLibraryPath(root, path string) string {
	value, err := filepath.Rel(root, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(norm.NFKC.String(value))
}

func normalizedRelativeLibraryPath(root, path string) string {
	return normalizePlatformPath(relativeLibraryPath(root, path))
}

func normalizePlatformPath(path string) string {
	value := norm.NFKC.String(filepath.ToSlash(path))
	if runtime.GOOS == "windows" {
		return strings.ToLower(value)
	}
	return value
}

func (file DiscoveredFile) String() string {
	if file.CuePath == "" {
		return file.RelativePath
	}
	return fmt.Sprintf("%s (CUE %s)", file.RelativePath, file.CuePath)
}
