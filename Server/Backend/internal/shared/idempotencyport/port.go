// Package idempotencyport holds the transport-neutral structs shared by the
// module-level idempotency ports. Each module keeps its own Idempotency
// interface but aliases these definitions so call sites stay unchanged.
package idempotencyport

import "encoding/json"

// Input is the request context for one idempotent operation.
type Input struct {
	ActorID string
	Scope   string
	Key     string
	Payload any
}

// Response is the raw HTTP result produced by an operation.
type Response struct {
	Status int
	Body   json.RawMessage
}

// Result is the stored or freshly produced operation result.
type Result struct {
	Status   int
	Body     json.RawMessage
	Replayed bool
}
