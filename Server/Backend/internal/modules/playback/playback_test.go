package playback

import "testing"

func TestSelectVariant(t *testing.T) {
	variants := []Variant{
		{ID: "lossless", Quality: "LOSSLESS", Bitrate: 900000},
		{ID: "standard", Quality: "STANDARD", Bitrate: 192000},
		{ID: "data", Quality: "DATA_SAVER", Bitrate: 64000},
		{ID: "high", Quality: "HIGH", Bitrate: 320000},
	}
	if got := SelectVariant(variants, QualityAuto); got == nil || got.ID != "lossless" {
		t.Fatalf("AUTO = %#v", got)
	}
	if got := SelectVariant(variants, QualityStandard); got == nil || got.ID != "standard" {
		t.Fatalf("STANDARD = %#v", got)
	}
	if got := SelectVariant(variants[:1], QualityDataSaver); got == nil || got.ID != "lossless" {
		t.Fatalf("fallback = %#v", got)
	}
}

func TestNormalizeCodecs(t *testing.T) {
	values, err := normalizeCodecs([]string{" FLAC ", "aac"})
	if err != nil || values[0] != "flac" {
		t.Fatalf("codecs = %#v %v", values, err)
	}
	if _, err := normalizeCodecs([]string{"aac", "AAC"}); err == nil {
		t.Fatal("expected duplicate codec error")
	}
}
