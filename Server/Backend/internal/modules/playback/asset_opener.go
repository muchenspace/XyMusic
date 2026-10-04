package playback

import (
	"io"
	"os"
	"time"
)

// AssetOpener opens a resolved audio source for HTTP range serving. The
// concrete implementation is an OS adapter; routes only depend on this port.
type AssetOpener interface {
	Open(path string) (io.ReadSeekCloser, int64, time.Time, error)
}

// AssetOpenError distinguishes the filesystem operation that failed so the
// transport layer can keep its original open/stat error messages.
type AssetOpenError struct {
	Operation string
	Err       error
}

func (failure *AssetOpenError) Error() string { return failure.Err.Error() }
func (failure *AssetOpenError) Unwrap() error { return failure.Err }

// OSAssetOpener opens source files directly from the filesystem.
type OSAssetOpener struct{}

func NewOSAssetOpener() *OSAssetOpener { return &OSAssetOpener{} }

func (*OSAssetOpener) Open(path string) (io.ReadSeekCloser, int64, time.Time, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, 0, time.Time{}, &AssetOpenError{Operation: "open", Err: err}
	}
	stat, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, 0, time.Time{}, &AssetOpenError{Operation: "stat", Err: err}
	}
	return file, stat.Size(), stat.ModTime(), nil
}
