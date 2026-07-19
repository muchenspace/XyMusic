package library

import (
	"regexp"
	"unicode/utf16"
)

var libraryUUIDPattern = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func validLibraryUUID(value string) bool {
	return libraryUUIDPattern.MatchString(value)
}

func javascriptStringLength(value string) int {
	length := 0
	for _, character := range value {
		length += utf16.RuneLen(character)
	}
	return length
}
