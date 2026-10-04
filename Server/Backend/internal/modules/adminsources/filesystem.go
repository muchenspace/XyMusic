package adminsources

import (
	"errors"
	"io"
	"os"
)

// OSDirectoryBrowser implements DirectoryBrowser with the os package. It is
// injected by the composition root so the service layer performs no direct
// filesystem I/O.
type OSDirectoryBrowser struct{}

func (OSDirectoryBrowser) Stat(path string) (os.FileInfo, error) { return os.Stat(path) }

func (OSDirectoryBrowser) ReadDir(path string) ([]os.DirEntry, error) { return os.ReadDir(path) }

// OSRootProbe implements RootProbe with the os package. The probe mirrors the
// historical validation sequence exactly: stat the directory, open it and read
// one entry, then create and remove a temporary file for read-write sources.
type OSRootProbe struct{}

func (OSRootProbe) Stat(path string) (os.FileInfo, error) { return os.Stat(path) }

func (OSRootProbe) Readable(path string) error {
	opened, err := os.Open(path)
	if err != nil {
		return err
	}
	_, readErr := opened.Readdirnames(1)
	closeErr := opened.Close()
	if (readErr != nil && !errors.Is(readErr, io.EOF)) || closeErr != nil {
		return errors.Join(readErr, closeErr)
	}
	return nil
}

func (OSRootProbe) Writable(path string) error {
	probe, err := os.CreateTemp(path, ".xymusic-write-probe-*")
	if err != nil {
		return err
	}
	probePath := probe.Name()
	return errors.Join(probe.Close(), os.Remove(probePath))
}
