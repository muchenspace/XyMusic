// Package mutationresult carries HTTP replay metadata without coupling a
// service to Gin. Routes expose Replayed through X-Idempotent-Replay.
package mutationresult

type Result[T any] struct {
	Body     T
	Replayed bool
}
