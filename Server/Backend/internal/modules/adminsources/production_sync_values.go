package adminsources

import (
	"strings"

	"golang.org/x/text/unicode/norm"
)

func normalizeCatalogText(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(norm.NFKC.String(value)), " "))
}

func catalogDate(value *string) any {
	if value == nil || *value == "" {
		return nil
	}
	if len(*value) == 4 {
		return *value + "-01-01"
	}
	if len(*value) == 7 {
		return *value + "-01"
	}
	return *value
}

func preferredFirst(values []string, preferred *string) []string {
	result := append([]string(nil), values...)
	if preferred == nil {
		return result
	}
	for index, value := range result {
		if value == *preferred {
			copy(result[1:index+1], result[0:index])
			result[0] = value
			break
		}
	}
	return result
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func sameOptionalString(left, right *string) bool {
	return left == nil && right == nil || left != nil && right != nil && *left == *right
}
