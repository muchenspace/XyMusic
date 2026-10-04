package library

import "regexp"

var libraryUUIDPattern = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func validLibraryUUID(value string) bool {
	return libraryUUIDPattern.MatchString(value)
}
