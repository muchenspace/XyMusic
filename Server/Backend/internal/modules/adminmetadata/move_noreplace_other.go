//go:build !windows && !linux

package adminmetadata

import "os"

func moveFileNoReplace(sourcePath, destinationPath string) error {
	// Hard-link creation is the portable no-replace primitive available on the
	// remaining platforms. Both paths are always in the same media directory.
	if err := os.Link(sourcePath, destinationPath); err != nil {
		return err
	}
	if err := os.Remove(sourcePath); err != nil {
		_ = os.Remove(destinationPath)
		return err
	}
	return nil
}
