package playback

import (
	"fmt"
	"strings"
)

// StreamProtocol identifies the representation a player is expected to
// consume. A progressive stream is a finite byte-addressable file; HLS is a
// time-addressable playlist whose segments may still be generated.
type StreamProtocol string

const (
	StreamProtocolProgressive StreamProtocol = "PROGRESSIVE"
	StreamProtocolHLS         StreamProtocol = "HLS"
)

func normalizeStreamProtocol(value StreamProtocol) (StreamProtocol, error) {
	switch StreamProtocol(strings.ToUpper(strings.TrimSpace(string(value)))) {
	case "", StreamProtocolProgressive:
		return StreamProtocolProgressive, nil
	case StreamProtocolHLS:
		return StreamProtocolHLS, nil
	default:
		return "", fmt.Errorf("streamProtocol is invalid")
	}
}
