package timeformat

import "time"

// Timestamp renders an API timestamp in the canonical v1 format: UTC with
// millisecond precision and a trailing Z. All admin/client DTOs that used a
// local formatTimestamp/formatTime helper share this representation.
func Timestamp(value time.Time) string {
	return value.UTC().Truncate(time.Millisecond).Format("2006-01-02T15:04:05.000Z")
}

// OptionalTimestamp renders a nullable timestamp, returning nil when the
// source pointer is nil.
func OptionalTimestamp(value *time.Time) *string {
	if value == nil {
		return nil
	}
	formatted := Timestamp(*value)
	return &formatted
}
