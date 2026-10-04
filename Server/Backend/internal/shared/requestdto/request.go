// Package requestdto holds request payloads shared by admin modules.
package requestdto

type VersionInput struct {
	ExpectedVersion int `json:"expectedVersion"`
}
