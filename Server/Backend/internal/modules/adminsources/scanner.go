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
	SidecarPaths []string
	ScanError    error
}

type FileSynchronizer interface {
	ProcessFile(context.Context, string, string, DiscoveredFile, time.Time) error
	ArchiveMissing(context.Context, string, time.Time, time.Time) (int, error)
}

type FilesystemScanner struct {
	synchronizer FileSynchronizer
	workers      int
	now          func() time.Time
}

func NewFilesystemScanner(synchronizer FileSynchronizer) (*FilesystemScanner, error) {
	return NewFilesystemScannerWithOptions(synchronizer, FilesystemScannerOptions{})
}

type FilesystemScannerOptions struct {
	Workers int
	Now     func() time.Time
}

func NewFilesystemScannerWithOptions(synchronizer FileSynchronizer, options FilesystemScannerOptions) (*FilesystemScanner, error) {
	if synchronizer == nil {
		return nil, errors.New("local library file synchronizer is required")
	}
	if options.Workers == 0 {
		options.Workers = 8
	}
	if options.Workers < 1 || options.Workers > 128 {
		return nil, errors.New("local library scanner workers must be from 1 to 128")
	}
	if options.Now == nil {
		options.Now = func() time.Time { return time.Now().UTC() }
	}
	return &FilesystemScanner{synchronizer: synchronizer, workers: options.Workers, now: options.Now}, nil
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
	if cancelled, err := scanCancelled(ctx, input.IsCancelled); err != nil {
		return ScanResult{}, err
	} else if cancelled {
		return ScanResult{}, ErrScanCancelled
	}
	workContext, cancelWork := context.WithCancel(ctx)
	defer cancelWork()
	jobs := make(chan DiscoveredFile, len(files))
	results := make(chan error, len(files))
	for _, file := range files {
		jobs <- file
	}
	close(jobs)
	workerCount := min(scanner.workers, max(1, len(files)))
	var group sync.WaitGroup
	for index := 0; index < workerCount; index++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for file := range jobs {
				if file.ScanError != nil {
					recordErr := scanner.synchronizer.ProcessFile(workContext, input.RootID, input.ScanRunID, file, startedAt)
					results <- errors.Join(file.ScanError, recordErr)
					continue
				}
				cancelled, checkErr := scanCancelled(workContext, input.IsCancelled)
				if checkErr != nil {
					results <- checkErr
				} else if cancelled {
					results <- ErrScanCancelled
				} else {
					results <- scanner.synchronizer.ProcessFile(workContext, input.RootID, input.ScanRunID, file, startedAt)
				}
			}
		}()
	}
	group.Wait()
	close(results)
	var progressErr error
	for processErr := range results {
		if errors.Is(processErr, ErrScanCancelled) || errors.Is(processErr, context.Canceled) {
			cancelWork()
			return ScanResult{}, ErrScanCancelled
		}
		progress.ProcessedFiles++
		if processErr != nil {
			progress.FailedFiles++
		}
		if input.OnProgress != nil && progressErr == nil {
			if err := input.OnProgress(ctx, progress); err != nil {
				progressErr = err
				cancelWork()
			}
		}
	}
	if progressErr != nil {
		return ScanResult{}, progressErr
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
	files := make([]DiscoveredFile, 0)
	audioPaths := make([]string, 0)
	sidecarsByDirectory := make(map[string][]string)
	cueOwned := make(map[string]string)
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
			return nil
		}
		extension := strings.ToLower(filepath.Ext(entry.Name()))
		if extension == ".cue" {
			cuePath := path
			references, parseErr := cueReferences(cuePath)
			if parseErr != nil {
				files = append(files, DiscoveredFile{AudioPath: cuePath, RelativePath: relativeLibraryPath(root, cuePath), CuePath: cuePath, ScanError: parseErr})
				return nil
			}
			for _, reference := range references {
				target, resolveErr := resolveFileWithinRoot(root, filepath.Join(filepath.Dir(cuePath), reference))
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
			return nil
		}
		if _, supported := supportedAudioExtensions[extension]; supported {
			audioPaths = append(audioPaths, path)
			return nil
		}
		if extension == ".lrc" || extension == ".txt" {
			directoryKey := normalizePlatformPath(filepath.Dir(path))
			sidecarsByDirectory[directoryKey] = append(sidecarsByDirectory[directoryKey], path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	for _, path := range audioPaths {
		if _, owned := cueOwned[normalizePlatformPath(path)]; owned {
			continue
		}
		relative := normalizedRelativeLibraryPath(root, path)
		if matchesPatterns(relative, include, exclude) {
			files = append(files, DiscoveredFile{AudioPath: path, RelativePath: relativeLibraryPath(root, path), SidecarPaths: append([]string{}, sidecarsByDirectory[normalizePlatformPath(filepath.Dir(path))]...)})
		}
	}
	for index := range files {
		if files[index].AudioPath == "" || files[index].ScanError != nil {
			continue
		}
		files[index].SidecarPaths = append([]string{}, sidecarsByDirectory[normalizePlatformPath(filepath.Dir(files[index].AudioPath))]...)
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
