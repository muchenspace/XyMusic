package httpx

import "unicode/utf16"

// JavascriptStringLength returns the length of value in UTF-16 code units.
// It matches JavaScript's String.prototype.length, which API validation uses
// for user-visible character limits.
func JavascriptStringLength(value string) int {
	return len(utf16.Encode([]rune(value)))
}
