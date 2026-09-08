package playback

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

type mockResolver struct {
	source   *ResolvedAudioSource
	err      error
	exists   bool
	existErr error
}

func (m *mockResolver) ResolveSource(_ context.Context, _ string) (*ResolvedAudioSource, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.source, nil
}

func (m *mockResolver) PublishedTrackExists(_ context.Context, _ string) (bool, error) {
	return m.exists, m.existErr
}

func TestTicketSignerAndVerification(t *testing.T) {
	signer, err := NewTicketSigner("01234567890123456789012345678901")
	if err != nil {
		t.Fatal(err)
	}

	claims := TicketClaims{
		UserID:    uuid.NewString(),
		TrackID:   uuid.NewString(),
		ExpiresAt: time.Now().Add(5 * time.Minute).Unix(),
	}

	token, err := signer.Sign(claims)
	if err != nil {
		t.Fatalf("sign error: %v", err)
	}

	verified, err := signer.Verify(token)
	if err != nil {
		t.Fatalf("verify error: %v", err)
	}
	if verified.UserID != claims.UserID || verified.TrackID != claims.TrackID {
		t.Fatalf("verified claims mismatch: %+v", verified)
	}

	// Tampered token
	tampered := token[:len(token)-4] + "xxxx"
	if _, err := signer.Verify(tampered); err == nil {
		t.Fatal("expected error on tampered signature, got nil")
	}

	// Expired token
	expiredClaims := claims
	expiredClaims.ExpiresAt = time.Now().Add(-1 * time.Minute).Unix()
	expiredToken, _ := signer.Sign(expiredClaims)
	if _, err := signer.Verify(expiredToken); err == nil {
		t.Fatal("expected error on expired token, got nil")
	}
}

func TestCreateGrantReturnsDirectStreamURL(t *testing.T) {
	signer, _ := NewTicketSigner("01234567890123456789012345678901")
	trackID := uuid.NewString()
	userID := uuid.NewString()
	sampleRate := 44100

	source := &ResolvedAudioSource{
		TrackID:        trackID,
		SourcePath:     "music/sample.flac",
		DurationMs:     180000,
		SizeBytes:      1024000,
		Bitrate:        320000,
		SampleRate:     &sampleRate,
		ChecksumSHA256: "abc123sha",
	}

	service, err := NewService(&mockResolver{source: source, exists: true}, signer, 15*time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	grant, err := service.CreateGrant(context.Background(), userID, trackID)
	if err != nil {
		t.Fatalf("create grant error: %v", err)
	}

	if grant.TrackID != trackID {
		t.Errorf("expected trackID %s, got %s", trackID, grant.TrackID)
	}
	if grant.MimeType != "audio/flac" {
		t.Errorf("expected mime audio/flac, got %s", grant.MimeType)
	}
	if grant.Container != "flac" || grant.Codec != "flac" {
		t.Errorf("expected flac container and codec, got %s / %s", grant.Container, grant.Codec)
	}
	if grant.ContentLength == nil || *grant.ContentLength != 1024000 {
		t.Errorf("expected contentLength 1024000, got %v", grant.ContentLength)
	}
	if grant.DurationMs != 180000 {
		t.Errorf("expected duration 180000, got %d", grant.DurationMs)
	}
}

func TestSourceFormatForPath(t *testing.T) {
	cases := []struct {
		path     string
		codec    string
		mimeType string
	}{
		{"track.flac", "flac", "audio/flac"},
		{"track.mp3", "mp3", "audio/mpeg"},
		{"track.m4a", "m4a", "audio/mp4"},
		{"track.wav", "wav", "audio/wav"},
		{"track.ogg", "ogg", "audio/ogg"},
		{"track.opus", "opus", "audio/ogg"},
		{"track.unknown", "unknown", ""},
	}

	for _, tc := range cases {
		f := sourceFormatForPath(tc.path)
		if f.codec != tc.codec {
			t.Errorf("%s: expected codec %s, got %s", tc.path, tc.codec, f.codec)
		}
		if tc.mimeType != "" && f.mimeType != tc.mimeType {
			t.Errorf("%s: expected mime %s, got %s", tc.path, tc.mimeType, f.mimeType)
		}
	}
}
