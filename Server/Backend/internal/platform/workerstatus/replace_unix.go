//go:build !windows

package workerstatus

import "os"

func replaceFile(source, destination string) error {
	return os.Rename(source, destination)
}
