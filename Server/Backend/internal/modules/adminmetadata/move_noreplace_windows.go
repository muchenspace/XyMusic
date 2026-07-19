//go:build windows

package adminmetadata

import "golang.org/x/sys/windows"

func moveFileNoReplace(sourcePath, destinationPath string) error {
	source, err := windows.UTF16PtrFromString(sourcePath)
	if err != nil {
		return err
	}
	destination, err := windows.UTF16PtrFromString(destinationPath)
	if err != nil {
		return err
	}
	// MoveFile intentionally omits MOVEFILE_REPLACE_EXISTING, so a file that
	// appears at the destination during recovery is never overwritten.
	return windows.MoveFile(source, destination)
}
