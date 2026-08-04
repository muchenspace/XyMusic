package mediapayload

import (
	"encoding/json"
	"testing"
)

func TestDecodeSourceProbeValidatesHintsAndKeepsOlderPayloadsCompatible(t *testing.T) {
	hint, ok := DecodeSourceProbe(json.RawMessage(`{"sourceProbe":{"durationMs":1250,"codec":" flac ","sampleRate":44100},"segmentStartMs":10}`))
	if !ok || hint.DurationMS != 1250 || hint.Codec != "flac" || hint.SampleRate == nil || *hint.SampleRate != 44100 {
		t.Fatalf("hint=%+v ok=%v", hint, ok)
	}
	for _, payload := range []json.RawMessage{
		[]byte(`{"segmentStartMs":10}`),
		[]byte(`{"sourceProbe":{"durationMs":0,"codec":"flac"}}`),
		[]byte(`{"sourceProbe":{"durationMs":1250,"codec":""}}`),
		[]byte(`{"sourceProbe":{"durationMs":1250,"codec":"flac"} trailing`),
	} {
		if _, ok := DecodeSourceProbe(payload); ok {
			t.Fatalf("accepted invalid or legacy payload %s", payload)
		}
	}
}
