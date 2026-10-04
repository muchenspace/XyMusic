// Package clock provides the production wall-clock implementation shared by
// modules that define their own narrow Clock ports.
package clock

import "time"

// System is a Clock backed by time.Now. It is stateless so the zero value is
// ready to use.
type System struct{}

func (System) Now() time.Time { return time.Now() }
