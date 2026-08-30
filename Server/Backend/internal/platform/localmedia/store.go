package localmedia

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"xymusic/server/internal/shared/apperror"
)

var (
	ErrPathTraversal = errors.New("path traversal detected")
	ErrSymlinkEscape = errors.New("symlink escape detected")
)

type Store struct {
	assetDir       string
	transcodeDir   string
	maxUploadBytes int64
}

func NewStore(assetDir, transcodeDir string, maxUploadBytes int64) (*Store, error) {
	if strings.TrimSpace(assetDir) == "" {
		return nil, errors.New("media asset directory is required")
	}
	if strings.TrimSpace(transcodeDir) == "" {
		return nil, errors.New("media transcode directory is required")
	}
	absAssetDir, err := filepath.Abs(assetDir)
	if err != nil {
		return nil, fmt.Errorf("resolve asset directory: %w", err)
	}
	absTranscodeDir, err := filepath.Abs(transcodeDir)
	if err != nil {
		return nil, fmt.Errorf("resolve transcode directory: %w", err)
	}
	if err := os.MkdirAll(absAssetDir, 0o755); err != nil {
		return nil, fmt.Errorf("initialize asset directory: %w", err)
	}
	if err := os.MkdirAll(absTranscodeDir, 0o755); err != nil {
		return nil, fmt.Errorf("initialize transcode directory: %w", err)
	}
	if maxUploadBytes <= 0 {
		maxUploadBytes = 1024 * 1024 * 1024
	}
	return &Store{
		assetDir:       filepath.Clean(absAssetDir),
		transcodeDir:   filepath.Clean(absTranscodeDir),
		maxUploadBytes: maxUploadBytes,
	}, nil
}

func (s *Store) AssetDirectory() string {
	return s.assetDir
}

func (s *Store) TranscodeDirectory() string {
	return s.transcodeDir
}

func (s *Store) Ping(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	testFile := filepath.Join(s.assetDir, ".ping_"+uuid.NewString())
	if err := os.WriteFile(testFile, []byte("ok"), 0o600); err != nil {
		return fmt.Errorf("verify asset directory writeability: %w", err)
	}
	_ = os.Remove(testFile)

	testTranscode := filepath.Join(s.transcodeDir, ".ping_"+uuid.NewString())
	if err := os.WriteFile(testTranscode, []byte("ok"), 0o600); err != nil {
		return fmt.Errorf("verify transcode directory writeability: %w", err)
	}
	_ = os.Remove(testTranscode)

	return nil
}

func (s *Store) ResolveAssetPath(relativePath string) (string, error) {
	return resolveSecurePath(s.assetDir, relativePath)
}

func (s *Store) ResolveTranscodePath(relativePath string) (string, error) {
	return resolveSecurePath(s.transcodeDir, relativePath)
}

func resolveSecurePath(baseDir, relativePath string) (string, error) {
	relativePath = strings.TrimSpace(relativePath)
	if relativePath == "" {
		return "", apperror.Validation("path must not be empty")
	}
	if filepath.VolumeName(relativePath) != "" {
		return "", ErrPathTraversal
	}
	cleanedRel := filepath.Clean(filepath.FromSlash(relativePath))
	if strings.HasPrefix(cleanedRel, "..") || filepath.IsAbs(cleanedRel) {
		return "", ErrPathTraversal
	}
	fullPath := filepath.Join(baseDir, cleanedRel)
	cleanFull := filepath.Clean(fullPath)
	if !isSubdirectory(baseDir, cleanFull) {
		return "", ErrPathTraversal
	}
	// Check symlinks if the file or any parent exists
	evalBase, err := filepath.EvalSymlinks(baseDir)
	if err == nil {
		evalFull, err := filepath.EvalSymlinks(cleanFull)
		if err == nil {
			if !isSubdirectory(evalBase, evalFull) {
				return "", ErrSymlinkEscape
			}
		}
	}
	return cleanFull, nil
}

func isSubdirectory(base, target string) bool {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return false
	}
	return !strings.HasPrefix(rel, "..") && rel != ".."
}

func (s *Store) CreateTempUploadFile(prefix string) (tempPath string, file *os.File, err error) {
	tempDir := filepath.Join(s.assetDir, "temp")
	if err := os.MkdirAll(tempDir, 0o755); err != nil {
		return "", nil, fmt.Errorf("create upload temp dir: %w", err)
	}
	f, err := os.CreateTemp(tempDir, prefix+"_*.partial")
	if err != nil {
		return "", nil, fmt.Errorf("create temp upload file: %w", err)
	}
	return f.Name(), f, nil
}

// WriteUploadStream writes streaming input to a temp partial file while enforcing size limit and SHA-256 validation.
func (s *Store) WriteUploadStream(
	ctx context.Context,
	reader io.Reader,
	expectedSize int64,
	expectedSHA256 string,
	targetTempPath string,
) (int64, string, error) {
	if expectedSize > s.maxUploadBytes {
		return 0, "", apperror.PayloadTooLarge("Upload size exceeds maximum allowed limit")
	}
	parentDir := filepath.Dir(targetTempPath)
	if err := os.MkdirAll(parentDir, 0o755); err != nil {
		return 0, "", fmt.Errorf("create temp directory: %w", err)
	}
	file, err := os.OpenFile(targetTempPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return 0, "", fmt.Errorf("open upload target: %w", err)
	}

	hasher := sha256.New()
	limitReader := io.LimitReader(reader, expectedSize+1)
	writer := io.MultiWriter(file, hasher)

	buffer := make([]byte, 64*1024)
	var totalWritten int64
	var streamErr error
	for {
		if err := ctx.Err(); err != nil {
			streamErr = err
			break
		}
		n, readErr := limitReader.Read(buffer)
		if n > 0 {
			wN, writeErr := writer.Write(buffer[:n])
			if writeErr != nil {
				streamErr = fmt.Errorf("write upload stream: %w", writeErr)
				break
			}
			totalWritten += int64(wN)
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			streamErr = fmt.Errorf("read upload stream: %w", readErr)
			break
		}
	}

	_ = file.Close()

	if streamErr != nil {
		_ = os.Remove(targetTempPath)
		return 0, "", streamErr
	}

	if expectedSize > 0 && totalWritten != expectedSize {
		_ = os.Remove(targetTempPath)
		return 0, "", apperror.Validation("Upload size does not match expected size")
	}

	calculatedChecksum := hex.EncodeToString(hasher.Sum(nil))
	if expectedSHA256 != "" && !strings.EqualFold(calculatedChecksum, expectedSHA256) {
		_ = os.Remove(targetTempPath)
		return 0, "", apperror.Validation("Upload SHA-256 checksum does not match expected checksum")
	}

	return totalWritten, calculatedChecksum, nil
}

func (s *Store) CommitUpload(ctx context.Context, tempPath string, relativeDestPath string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	destPath, err := s.ResolveAssetPath(relativeDestPath)
	if err != nil {
		return "", err
	}
	destDir := filepath.Dir(destPath)
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", fmt.Errorf("create destination directory: %w", err)
	}
	if err := os.Rename(tempPath, destPath); err != nil {
		// Try copy if rename fails across cross-device volumes
		if copyErr := copyFile(tempPath, destPath); copyErr != nil {
			return "", fmt.Errorf("commit upload file: %w", copyErr)
		}
		_ = os.Remove(tempPath)
	}
	cleanRel, _ := filepath.Rel(s.assetDir, destPath)
	return filepath.ToSlash(cleanRel), nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}

func (s *Store) OpenAsset(relativePath string) (*os.File, error) {
	fullPath, err := s.ResolveAssetPath(relativePath)
	if err != nil {
		return nil, err
	}
	return os.Open(fullPath)
}

func (s *Store) StatAsset(relativePath string) (os.FileInfo, error) {
	fullPath, err := s.ResolveAssetPath(relativePath)
	if err != nil {
		return nil, err
	}
	return os.Stat(fullPath)
}

func (s *Store) DeleteAsset(relativePath string) error {
	fullPath, err := s.ResolveAssetPath(relativePath)
	if err != nil {
		return err
	}
	err = os.Remove(fullPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (s *Store) DeleteTranscodeFile(relativePath string) error {
	fullPath, err := s.ResolveTranscodePath(relativePath)
	if err != nil {
		return err
	}
	err = os.Remove(fullPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (s *Store) DownloadToFile(ctx context.Context, relativePath string, targetPath string, maxBytes int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	fullPath, err := s.ResolveAssetPath(relativePath)
	if err != nil {
		return err
	}
	src, err := os.Open(fullPath)
	if err != nil {
		return err
	}
	defer src.Close()
	destDir := filepath.Dir(targetPath)
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return err
	}
	dst, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer dst.Close()
	var reader io.Reader = src
	if maxBytes > 0 {
		reader = io.LimitReader(src, maxBytes)
	}
	_, err = io.Copy(dst, reader)
	return err
}

func (s *Store) Open(ctx context.Context, relativePath string) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return s.OpenAsset(relativePath)
}

func (s *Store) UploadFile(ctx context.Context, relativeDestPath string, sourceFilePath string, contentType string, checksum string) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	destPath, err := s.ResolveAssetPath(relativeDestPath)
	if err != nil {
		return 0, err
	}
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return 0, fmt.Errorf("create dest directory: %w", err)
	}
	if err := copyFile(sourceFilePath, destPath); err != nil {
		return 0, fmt.Errorf("upload local file: %w", err)
	}
	fi, err := os.Stat(destPath)
	if err != nil {
		return 0, err
	}
	return fi.Size(), nil
}
