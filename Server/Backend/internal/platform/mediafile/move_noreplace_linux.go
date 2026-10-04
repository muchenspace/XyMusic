//go:build linux

package mediafile

import "golang.org/x/sys/unix"

func MoveFileNoReplace(sourcePath, destinationPath string) error {
	return unix.Renameat2(
		unix.AT_FDCWD,
		sourcePath,
		unix.AT_FDCWD,
		destinationPath,
		unix.RENAME_NOREPLACE,
	)
}
