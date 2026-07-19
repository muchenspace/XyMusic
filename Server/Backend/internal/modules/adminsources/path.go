package adminsources

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"

	"xymusic/server/internal/shared/apperror"
	"xymusic/server/internal/shared/pagination"
)

func validateRootInput(rootDirectory string, input RootMutation) (RootMutation, error) {
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" || utf8.RuneCountInString(input.Name) > 120 {
		return RootMutation{}, apperror.Validation("Music source name is invalid")
	}
	if input.Mode != RootModeReadOnly && input.Mode != RootModeReadWrite {
		return RootMutation{}, apperror.Validation("Music source mode is invalid")
	}
	if input.ScanIntervalMinutes != nil && (*input.ScanIntervalMinutes < 5 || *input.ScanIntervalMinutes > 10080) {
		return RootMutation{}, apperror.Validation("Scan interval must be 5 to 10080 minutes")
	}
	path := strings.TrimSpace(input.Path)
	if path == "" || utf8.RuneCountInString(path) > 4000 {
		return RootMutation{}, apperror.Validation("Music source path is invalid")
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(rootDirectory, path)
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return RootMutation{}, apperror.New(apperror.CodeValidationError, "Music source path is invalid", apperror.WithCause(err))
	}
	path = filepath.Clean(absolute)
	metadata, err := os.Stat(path)
	if err != nil || !metadata.IsDir() {
		return RootMutation{}, apperror.New(apperror.CodeValidationError, "Music source path is not a directory", apperror.WithCause(err))
	}
	opened, err := os.Open(path)
	if err != nil {
		return RootMutation{}, apperror.New(apperror.CodeValidationError, "Music source path is not readable", apperror.WithCause(err))
	}
	_, readErr := opened.Readdirnames(1)
	closeErr := opened.Close()
	if (readErr != nil && !errors.Is(readErr, io.EOF)) || closeErr != nil {
		return RootMutation{}, apperror.New(apperror.CodeValidationError, "Music source path is not readable", apperror.WithCause(errors.Join(readErr, closeErr)))
	}
	if input.Mode == RootModeReadWrite {
		probe, err := os.CreateTemp(path, ".xymusic-write-probe-*")
		if err != nil {
			return RootMutation{}, apperror.New(apperror.CodeValidationError, "Music source path is not readable and writable", apperror.WithCause(err))
		}
		probePath := probe.Name()
		if err := errors.Join(probe.Close(), os.Remove(probePath)); err != nil {
			return RootMutation{}, apperror.New(apperror.CodeValidationError, "Music source path is not readable and writable", apperror.WithCause(err))
		}
	}
	input.IncludePatterns, err = validatePatterns(input.IncludePatterns)
	if err != nil {
		return RootMutation{}, err
	}
	input.ExcludePatterns, err = validatePatterns(input.ExcludePatterns)
	if err != nil {
		return RootMutation{}, err
	}
	input.Path = path
	input.NormalizedPath = normalizeRootPath(path)
	if input.Enabled {
		input.Status = RootStatusUnknown
	} else {
		input.Status = RootStatusDisabled
	}
	return input, nil
}

func browseDirectory(rootDirectory, value string, page, pageSize, offset int) (BrowseDTO, error) {
	path := strings.TrimSpace(value)
	if path == "" {
		path = rootDirectory
	} else if !filepath.IsAbs(path) {
		path = filepath.Join(rootDirectory, path)
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return BrowseDTO{}, apperror.Validation("Browse path is not a directory")
	}
	path = filepath.Clean(absolute)
	metadata, err := os.Stat(path)
	if err != nil || !metadata.IsDir() {
		return BrowseDTO{}, apperror.New(apperror.CodeValidationError, "Browse path is not a directory", apperror.WithCause(err))
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return BrowseDTO{}, apperror.New(apperror.CodeValidationError, "Browse path is not a directory", apperror.WithCause(err))
	}
	directories := make([]DirectoryDTO, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			directories = append(directories, DirectoryDTO{Name: entry.Name(), Path: filepath.Join(path, entry.Name())})
		}
	}
	sort.Slice(directories, func(i, j int) bool { return directories[i].Name < directories[j].Name })
	total := len(directories)
	start := min(offset, total)
	end := min(start+pageSize, total)
	directories = directories[start:end]
	return BrowseDTO{
		Path: path, Directories: directories, Page: page, PageSize: pageSize,
		Total: total, TotalPages: pagination.BoundedTotalPages(total, pageSize),
	}, nil
}

func validatePatterns(patterns []string) ([]string, error) {
	if patterns == nil {
		return nil, apperror.Validation("Music source patterns are required")
	}
	if len(patterns) > 100 {
		return nil, apperror.Validation("A music source cannot contain more than 100 patterns")
	}
	seen := make(map[string]struct{}, len(patterns))
	result := make([]string, 0, len(patterns))
	for _, pattern := range patterns {
		value := strings.TrimSpace(pattern)
		if _, err := compileLibraryGlob(value); err != nil {
			return nil, apperror.Validation("Music source pattern is invalid")
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result, nil
}

func compileLibraryGlob(pattern string) (*regexp.Regexp, error) {
	value := strings.ReplaceAll(strings.TrimSpace(norm.NFKC.String(pattern)), "\\", "/")
	if value == "" || utf8.RuneCountInString(value) > 500 {
		return nil, errors.New("library source pattern is invalid")
	}
	var expression strings.Builder
	expression.WriteByte('^')
	runes := []rune(value)
	for index := 0; index < len(runes); index++ {
		character := runes[index]
		if character == '*' && index+1 < len(runes) && runes[index+1] == '*' {
			if index+2 < len(runes) && runes[index+2] == '/' {
				expression.WriteString("(?:.*/)?")
				index += 2
			} else {
				expression.WriteString(".*")
				index++
			}
		} else if character == '*' {
			expression.WriteString("[^/]*")
		} else if character == '?' {
			expression.WriteString("[^/]")
		} else {
			expression.WriteString(regexp.QuoteMeta(string(character)))
		}
	}
	expression.WriteByte('$')
	return regexp.Compile(expression.String())
}

func normalizeRootPath(path string) string {
	absolute, err := filepath.Abs(path)
	if err != nil {
		absolute = filepath.Clean(path)
	}
	portable := filepath.ToSlash(norm.NFKC.String(filepath.Clean(absolute)))
	volumeRoot := filepath.VolumeName(absolute) + string(filepath.Separator)
	root := filepath.ToSlash(norm.NFKC.String(filepath.Clean(volumeRoot)))
	if portable != root {
		portable = strings.TrimSuffix(portable, "/")
	}
	if runtime.GOOS == "windows" {
		portable = strings.ToLower(portable)
	}
	return portable
}

func resolveFileWithinRoot(rootPath, candidatePath string) (string, error) {
	root, err := filepath.Abs(rootPath)
	if err != nil {
		return "", err
	}
	candidate, err := filepath.Abs(candidatePath)
	if err != nil {
		return "", err
	}
	if !pathWithinRoot(root, candidate) {
		return "", errors.New("CUE referenced audio outside the configured library root")
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	realCandidate, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", err
	}
	if !pathWithinRoot(realRoot, realCandidate) {
		return "", errors.New("CUE referenced audio outside the configured library root")
	}
	return candidate, nil
}

func pathWithinRoot(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	if err != nil || relative == "." || relative == ".." || filepath.IsAbs(relative) {
		return false
	}
	return !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
