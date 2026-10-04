package adminmetadata

import (
	"xymusic/server/internal/platform/mediafile"
)

type (
	ProbeOutput        = mediafile.ProbeOutput
	ProbeStream        = mediafile.ProbeStream
	StreamFingerprint  = mediafile.StreamFingerprint
	ProbedMetadataFile = mediafile.ProbedMetadataFile
	ProcessResult      = mediafile.ProcessResult
	ProcessRunner      = mediafile.ProcessRunner
	OSProcessRunner    = mediafile.OSProcessRunner
)

var (
	ProbeMetadataFile   = mediafile.ProbeMetadataFile
	RemuxMetadataToFile = mediafile.RemuxMetadataToFile
	VerifyMetadataRemux = mediafile.VerifyMetadataRemux
	moveFileNoReplace   = mediafile.MoveFileNoReplace
)
