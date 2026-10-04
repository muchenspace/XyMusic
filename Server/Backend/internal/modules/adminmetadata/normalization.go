package adminmetadata

import (
	"xymusic/server/internal/platform/mediafile"
)

var (
	NormalizeMetadataSnapshot  = mediafile.NormalizeMetadataSnapshot
	NormalizeMetadataPatch     = mediafile.NormalizeMetadataPatch
	NormalizeMetadataOverrides = mediafile.NormalizeMetadataOverrides
	ApplyMetadataOverrides     = mediafile.ApplyMetadataOverrides
	UpdateMetadataOverrides    = mediafile.UpdateMetadataOverrides
	MetadataChangedFields      = mediafile.MetadataChangedFields
	MetadataSnapshotsEqual     = mediafile.MetadataSnapshotsEqual
	MetadataOverridesForTarget = mediafile.MetadataOverridesForTarget
	decodeSnapshot             = mediafile.DecodeSnapshot
	decodeOverrides            = mediafile.DecodeOverrides
	encodeJSON                 = mediafile.EncodeJSON
	normalizeLookup            = mediafile.NormalizeLookup
	javascriptLength           = mediafile.JavaScriptLength
	sortedOverrideFields       = mediafile.SortedOverrideFields
	validCreditRole            = mediafile.ValidCreditRole
	exactInteger               = mediafile.ExactInteger
	floatingNumber             = mediafile.FloatingNumber
	stableEqual                = mediafile.StableEqual
	cleanMultiline             = mediafile.CleanMultiline
)
