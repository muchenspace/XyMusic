package adminmetadata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/text/unicode/norm"

	"xymusic/server/internal/platform/processio"
)

const (
	metadataProbeTimeout = 30 * time.Second
	metadataRemuxTimeout = 10 * time.Minute
	maximumToolStdout    = 4 * 1024 * 1024
	maximumToolStderr    = 256 * 1024
)

type ProbeOutput struct {
	Format *struct {
		Duration string         `json:"duration"`
		Tags     map[string]any `json:"tags"`
	} `json:"format"`
	Streams []ProbeStream `json:"streams"`
}

type ProbeStream struct {
	Index       *int           `json:"index"`
	CodecType   string         `json:"codec_type"`
	CodecName   string         `json:"codec_name"`
	CodecTag    string         `json:"codec_tag_string"`
	Duration    string         `json:"duration"`
	SampleRate  string         `json:"sample_rate"`
	Channels    *int           `json:"channels"`
	Width       *int           `json:"width"`
	Height      *int           `json:"height"`
	Disposition map[string]int `json:"disposition"`
	Tags        map[string]any `json:"tags"`
}

type StreamFingerprint struct {
	Index           int     `json:"index"`
	CodecType       string  `json:"codecType"`
	CodecName       string  `json:"codecName"`
	CodecTag        *string `json:"codecTag"`
	SampleRate      *int    `json:"sampleRate"`
	Channels        *int    `json:"channels"`
	Width           *int    `json:"width"`
	Height          *int    `json:"height"`
	AttachedPicture bool    `json:"attachedPicture"`
}

type ProbedMetadataFile struct {
	Metadata   MetadataSnapshot
	DurationMS *int64
	Streams    []StreamFingerprint
}

type OSProcessRunner struct{}

func (OSProcessRunner) Run(
	ctx context.Context,
	executable string,
	arguments []string,
	timeout time.Duration,
) (ProcessResult, error) {
	commandContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	command := exec.CommandContext(commandContext, executable, arguments...)
	command.WaitDelay = 5 * time.Second
	stdout := processio.NewHeadBuffer(maximumToolStdout)
	stderr := processio.NewTailBuffer(maximumToolStderr)
	command.Stdout = stdout
	command.Stderr = stderr
	err := command.Run()
	result := ProcessResult{
		Stdout: stdout.String(), Stderr: stderr.String(), StdoutTruncated: stdout.Truncated(),
	}
	if ctx.Err() != nil {
		if cause := context.Cause(ctx); cause != nil {
			return ProcessResult{}, cause
		}
		return ProcessResult{}, ctx.Err()
	}
	if errors.Is(commandContext.Err(), context.DeadlineExceeded) {
		result.TimedOut = true
		result.ExitCode = -1
		return result, nil
	}
	if err == nil {
		return result, nil
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		result.ExitCode = exitError.ExitCode()
		return result, nil
	}
	return ProcessResult{}, err
}

func ProbeMetadataFile(
	ctx context.Context,
	path, ffprobePath string,
	runner ProcessRunner,
) (ProbedMetadataFile, error) {
	result, err := runner.Run(ctx, ffprobePath, []string{
		"-v", "error", "-show_format", "-show_streams", "-of", "json", path,
	}, metadataProbeTimeout)
	if err != nil {
		return ProbedMetadataFile{}, err
	}
	if result.TimedOut {
		return ProbedMetadataFile{}, NewWritebackError("FFPROBE_TIMEOUT", "FFprobe exceeded 30 seconds")
	}
	if result.ExitCode != 0 {
		return ProbedMetadataFile{}, NewWritebackError("FFPROBE_FAILED", safeToolError(result.Stderr))
	}
	if result.StdoutTruncated {
		return ProbedMetadataFile{}, NewWritebackError(
			"FFPROBE_OUTPUT_TOO_LARGE",
			"FFprobe returned more metadata than the server can safely process",
		)
	}
	var probe ProbeOutput
	if err := json.Unmarshal([]byte(result.Stdout), &probe); err != nil {
		return ProbedMetadataFile{}, wrapWritebackError("FFPROBE_INVALID_OUTPUT", "FFprobe returned invalid JSON", err)
	}
	fallback := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	return metadataFileFromProbe(probe, fallback)
}

func MetadataSnapshotFromProbe(probe ProbeOutput, fallbackTitle string) (MetadataSnapshot, error) {
	result, err := metadataFileFromProbe(probe, fallbackTitle)
	return result.Metadata, err
}

func RemuxMetadataToFile(
	ctx context.Context,
	sourcePath, outputPath, artworkPath, ffmpegPath string,
	metadata MetadataSnapshot,
	runner ProcessRunner,
) error {
	containerArguments, format, err := ffmpegContainerArguments(sourcePath)
	if err != nil {
		return err
	}
	metadataPath, err := createFFMetadataInput(outputPath, metadata)
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(metadataPath) }()

	const metadataInputIndex = 1
	arguments := []string{
		"-nostdin", "-v", "error", "-y",
		"-i", sourcePath,
		"-f", "ffmetadata", "-i", metadataPath,
	}
	if artworkPath == "" {
		arguments = append(arguments, "-map", "0")
	} else {
		arguments = append(arguments,
			"-i", artworkPath,
			"-map", "0:a", "-map", "2:v:0",
			"-disposition:v:0", "attached_pic",
		)
	}
	arguments = append(arguments, "-map_metadata", strconv.Itoa(metadataInputIndex)+":g")
	if format == "ogg" || format == "opus" {
		// Vorbis comments and OpusTags belong to the audio stream. Mapping only
		// global metadata leaves the original stream tags in place.
		arguments = append(arguments, "-map_metadata:s:a:0", strconv.Itoa(metadataInputIndex)+":g")
	}
	arguments = append(arguments, "-map_chapters", "0", "-c", "copy")
	arguments = append(arguments, containerArguments...)
	arguments = append(arguments, "-f", format, outputPath)
	result, err := runner.Run(ctx, ffmpegPath, arguments, metadataRemuxTimeout)
	if err != nil {
		return err
	}
	if result.TimedOut {
		return NewWritebackError("FFMPEG_TIMEOUT", "FFmpeg metadata writeback exceeded 10 minutes")
	}
	if result.ExitCode != 0 {
		return NewWritebackError("FFMPEG_WRITE_FAILED", safeToolError(result.Stderr))
	}
	return nil
}

func VerifyMetadataRemux(
	before, after ProbedMetadataFile,
	expected MetadataSnapshot,
) error {
	if expected.HasArtwork != after.Metadata.HasArtwork {
		return NewWritebackError(
			"ARTWORK_VERIFICATION_FAILED",
			"Embedded artwork was not preserved during metadata writeback",
		)
	}
	if !MetadataSnapshotsEqual(after.Metadata, expected) {
		return NewWritebackError(
			"METADATA_VERIFICATION_FAILED",
			"Written tags do not match the requested metadata",
		)
	}
	if !streamLayoutsEqual(before.Streams, after.Streams) {
		return NewWritebackError(
			"STREAM_VERIFICATION_FAILED",
			"The media stream layout changed during metadata writeback",
		)
	}
	if before.DurationMS != nil && after.DurationMS != nil {
		tolerance := math.Max(250, float64(*before.DurationMS)*0.001)
		if math.Abs(float64(*before.DurationMS-*after.DurationMS)) > tolerance {
			return NewWritebackError(
				"DURATION_VERIFICATION_FAILED",
				"The media duration changed during metadata writeback",
			)
		}
	}
	return nil
}

func streamLayoutsEqual(before, after []StreamFingerprint) bool {
	filter := func(streams []StreamFingerprint) []StreamFingerprint {
		result := make([]StreamFingerprint, 0, len(streams))
		for _, stream := range streams {
			if !stream.AttachedPicture {
				result = append(result, stream)
			}
		}
		return result
	}
	return stableEqual(filter(before), filter(after))
}

func metadataFileFromProbe(probe ProbeOutput, fallbackTitle string) (ProbedMetadataFile, error) {
	var audio *ProbeStream
	for index := range probe.Streams {
		if probe.Streams[index].CodecType == "audio" {
			audio = &probe.Streams[index]
			break
		}
	}
	var formatTags map[string]any
	var duration string
	if probe.Format != nil {
		formatTags = probe.Format.Tags
		duration = probe.Format.Duration
	}
	var streamTags map[string]any
	if audio != nil {
		streamTags = audio.Tags
	}
	tags := normalizedTags(streamTags, formatTags)
	primaryArtists := splitMetadataList(firstTag(tags, "artist", "artists"))
	taggedAlbumArtists := splitMetadataList(firstTag(tags, "album_artist", "albumartist"))
	featuredArtists := splitMetadataList(firstTag(tags, "featured_artist", "featuring"))
	credits := make([]MetadataCredit, 0)
	credits = append(credits, creditList(defaultIfEmpty(primaryArtists, []string{"Unknown Artist"}), CreditPrimary)...)
	credits = append(credits, creditList(featuredArtists, CreditFeatured)...)
	credits = append(credits, creditList(splitMetadataList(firstTag(tags, "composer")), CreditComposer)...)
	credits = append(credits, creditList(splitMetadataList(firstTag(tags, "lyricist", "writer")), CreditLyricist)...)
	credits = append(credits, creditList(splitMetadataList(firstTag(tags, "producer")), CreditProducer)...)
	trackNumber, trackTotal := parseNumberPair(firstTag(tags, "track", "tracknumber"))
	discNumber, discTotal := parseNumberPair(firstTag(tags, "disc", "discnumber"))
	lyricsTag, lyricsLanguage, lyricsFormat := firstLyricsTag(tags)
	lyricsContent := cleanMultiline(lyricsTag)
	var lyrics *MetadataLyrics
	if lyricsContent != "" {
		if lyricsFormat == "" {
			lyricsFormat = "PLAIN"
			if lrcTimestampPattern.MatchString(lyricsContent) {
				lyricsFormat = "LRC"
			}
		}
		language := normalizeLanguage(firstNonEmpty(lyricsLanguage, firstTag(tags, "language", "lyrics-language")))
		lyrics = &MetadataLyrics{Content: lyricsContent, Format: lyricsFormat, Language: language}
	}
	releaseDate := parseReleaseDateTag(firstTag(tags, "date", "year"))
	bpm := parseBPM(firstTag(tags, "bpm", "tbpm"))
	var isrc *string
	if value := strings.ToUpper(strings.ReplaceAll(cleanText(firstTag(tags, "isrc")), "-", "")); isrcPattern.MatchString(value) {
		isrc = &value
	}
	albumArtists := taggedAlbumArtists
	if len(albumArtists) == 0 {
		if truthyTag(firstTag(tags, "compilation", "itunescompilation")) && firstTag(tags, "album") != "" {
			albumArtists = []string{"Various Artists"}
		} else {
			albumArtists = primaryArtists
		}
	}
	hasArtwork := false
	streams := make([]StreamFingerprint, 0, len(probe.Streams))
	for position, stream := range probe.Streams {
		attached := stream.Disposition["attached_pic"] == 1
		if stream.CodecType == "video" {
			comment := strings.ToLower(firstTag(normalizedTags(stream.Tags, nil), "comment"))
			if attached || strings.Contains(comment, "cover") {
				hasArtwork = true
			}
		}
		index := position
		if stream.Index != nil {
			index = *stream.Index
		}
		codecType := stream.CodecType
		if codecType == "" {
			codecType = "unknown"
		}
		codecName := stream.CodecName
		if codecName == "" {
			codecName = "unknown"
		}
		streams = append(streams, StreamFingerprint{
			Index: index, CodecType: codecType, CodecName: codecName,
			CodecTag:   nullableCleanText(stream.CodecTag),
			SampleRate: positiveIntegerPointer(stream.SampleRate), Channels: positiveIntPointer(stream.Channels),
			Width: positiveIntPointer(stream.Width), Height: positiveIntPointer(stream.Height),
			AttachedPicture: attached,
		})
	}
	value := map[string]any{
		"title":   firstNonEmpty(cleanText(firstTag(tags, "title")), fallbackTitle),
		"credits": credits, "albumArtists": albumArtists,
		"album":       nullableString(cleanText(firstTag(tags, "album"))),
		"releaseDate": releaseDate, "trackNumber": trackNumber, "trackTotal": trackTotal,
		"discNumber": discNumber, "discTotal": discTotal,
		"genres": splitMetadataList(firstTag(tags, "genre")), "bpm": bpm,
		"isrc": isrc, "comment": nullableString(cleanMultiline(firstTag(tags, "comment"))),
		"copyright": nullableString(cleanMultiline(firstTag(tags, "copyright"))),
		"lyrics":    lyrics, "hasArtwork": hasArtwork,
	}
	metadata, err := NormalizeMetadataSnapshot(value)
	if err != nil {
		return ProbedMetadataFile{}, err
	}
	var durationMS *int64
	if seconds, err := strconv.ParseFloat(duration, 64); err == nil && !math.IsNaN(seconds) && !math.IsInf(seconds, 0) && seconds >= 0 {
		value := int64(math.Round(seconds * 1_000))
		durationMS = &value
	}
	return ProbedMetadataFile{Metadata: metadata, DurationMS: durationMS, Streams: streams}, nil
}

func ffmetadataValues(metadata MetadataSnapshot) [][2]string {
	byRole := func(role CreditRole) string {
		values := make([]string, 0)
		for _, credit := range metadata.Credits {
			if credit.Role == role {
				values = append(values, credit.Name)
			}
		}
		return strings.Join(values, "; ")
	}
	numberPair := func(number, total *int) string {
		if number == nil {
			return ""
		}
		if total == nil {
			return strconv.Itoa(*number)
		}
		return fmt.Sprintf("%d/%d", *number, *total)
	}
	featured := byRole(CreditFeatured)
	lyricists := byRole(CreditLyricist)
	bpm := ""
	if metadata.BPM != nil {
		bpm = strconv.FormatFloat(*metadata.BPM, 'f', -1, 64)
	}
	lyrics := lyricsValue(metadata.Lyrics)
	lyricsFormat := lyricsFormatValue(metadata.Lyrics)
	syncedLyrics := ""
	unsyncedLyrics := ""
	if lyricsFormat == "LRC" {
		syncedLyrics = lyrics
	} else if lyricsFormat == "PLAIN" {
		unsyncedLyrics = lyrics
	}
	return [][2]string{
		{"title", metadata.Title}, {"artist", byRole(CreditPrimary)},
		{"featured_artist", featured}, {"featuring", featured},
		{"composer", byRole(CreditComposer)}, {"lyricist", lyricists}, {"writer", lyricists},
		{"producer", byRole(CreditProducer)}, {"album_artist", strings.Join(metadata.AlbumArtists, "; ")},
		{"albumartist", strings.Join(metadata.AlbumArtists, "; ")},
		{"album", pointerValue(metadata.Album)}, {"date", pointerValue(metadata.ReleaseDate)},
		{"track", numberPair(metadata.TrackNumber, metadata.TrackTotal)},
		{"tracknumber", numberPair(metadata.TrackNumber, metadata.TrackTotal)},
		{"disc", numberPair(metadata.DiscNumber, metadata.DiscTotal)},
		{"discnumber", numberPair(metadata.DiscNumber, metadata.DiscTotal)},
		{"genre", strings.Join(metadata.Genres, "; ")}, {"bpm", bpm}, {"tbpm", bpm},
		{"isrc", pointerValue(metadata.ISRC)}, {"comment", pointerValue(metadata.Comment)},
		{"copyright", pointerValue(metadata.Copyright)},
		{"lyrics", lyrics}, {"syncedlyrics", syncedLyrics}, {"unsyncedlyrics", unsyncedLyrics},
		{"lyrics_format", lyricsFormat}, {"lyrics-format", lyricsFormat},
		{"language", lyricsLanguageValue(metadata.Lyrics)},
		{"lyrics-language", lyricsLanguageValue(metadata.Lyrics)},
	}
}

func createFFMetadataInput(outputPath string, metadata MetadataSnapshot) (string, error) {
	file, err := os.CreateTemp(filepath.Dir(outputPath), ".xymusic-metadata-*.ffmeta")
	if err != nil {
		return "", filesystemWritebackError(err, "Unable to create temporary metadata input")
	}
	path := file.Name()
	cleanup := func() {
		_ = file.Close()
		_ = os.Remove(path)
	}
	if _, err := file.WriteString(ffmetadataDocument(metadata)); err != nil {
		cleanup()
		return "", filesystemWritebackError(err, "Unable to write temporary metadata input")
	}
	if err := file.Close(); err != nil {
		cleanup()
		return "", filesystemWritebackError(err, "Unable to close temporary metadata input")
	}
	return path, nil
}

func ffmetadataDocument(metadata MetadataSnapshot) string {
	var document strings.Builder
	document.WriteString(";FFMETADATA1\n")
	for _, pair := range ffmetadataValues(metadata) {
		document.WriteString(pair[0])
		document.WriteByte('=')
		value := normalizeLineEndings(pair[1])
		writeFFMetadataValue(&document, value)
		if strings.HasSuffix(value, "\\") {
			// A terminal backslash is otherwise liable to consume the record
			// delimiter in the ffmetadata demuxer. Probe normalization removes
			// this protective trailing space again.
			document.WriteByte(' ')
		}
		document.WriteByte('\n')
	}
	return document.String()
}

func writeFFMetadataValue(destination *strings.Builder, value string) {
	value = normalizeLineEndings(value)
	for _, character := range value {
		switch character {
		case '\\', '=', ';', '#':
			destination.WriteByte('\\')
			destination.WriteRune(character)
		case '\n':
			destination.WriteString("\\\n")
		default:
			destination.WriteRune(character)
		}
	}
}

func ffmpegContainerArguments(sourcePath string) ([]string, string, error) {
	container, err := metadataContainer(sourcePath)
	if err != nil {
		return nil, "", err
	}
	switch container {
	case "mp4":
		// The iTunes-compatible ilst atoms preserve attached pictures. FFmpeg's
		// generic mdta mode drops covr/attached_pic streams while remuxing M4A.
		return nil, container, nil
	case "mp3":
		// ID3v2.3 reduces TDRC values such as YYYY-MM to TYER (YYYY only).
		return []string{"-id3v2_version", "4", "-write_id3v1", "0"}, container, nil
	default:
		return nil, container, nil
	}
}

func metadataContainer(sourcePath string) (string, error) {
	switch strings.ToLower(filepath.Ext(sourcePath)) {
	case ".flac":
		return "flac", nil
	case ".mp3":
		return "mp3", nil
	case ".m4a", ".mp4":
		return "mp4", nil
	case ".ogg":
		return "ogg", nil
	case ".opus":
		return "opus", nil
	default:
		return "", NewWritebackError("UNSUPPORTED_CONTAINER", "The source container cannot be written safely")
	}
}

func normalizedTags(sources ...map[string]any) map[string]string {
	result := make(map[string]string)
	for _, source := range sources {
		for key, value := range source {
			var text string
			switch typed := value.(type) {
			case string:
				text = typed
			case float64:
				text = strconv.FormatFloat(typed, 'f', -1, 64)
			case json.Number:
				text = string(typed)
			default:
				continue
			}
			result[strings.ToLower(strings.TrimSpace(norm.NFKC.String(key)))] = text
		}
	}
	return result
}

func firstTag(tags map[string]string, keys ...string) string {
	for _, key := range keys {
		if value := tags[key]; strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func firstLyricsTag(tags map[string]string) (string, string, string) {
	taggedFormat := normalizedLyricsFormat(firstTag(tags, "lyrics_format", "lyrics-format"))
	if value := firstTag(tags, "syncedlyrics"); value != "" {
		return value, "", firstNonEmpty(taggedFormat, "LRC")
	}
	if value := firstTag(tags, "unsyncedlyrics"); value != "" {
		return value, "", firstNonEmpty(taggedFormat, "PLAIN")
	}
	if value := firstTag(tags, "lyrics"); value != "" {
		return value, "", taggedFormat
	}
	keys := make([]string, 0)
	for key, value := range tags {
		if strings.HasPrefix(key, "lyrics-") && strings.TrimSpace(value) != "" {
			if key != "lyrics-format" && key != "lyrics-language" {
				keys = append(keys, key)
			}
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		if value := strings.TrimSpace(tags[key]); value != "" {
			return value, strings.TrimPrefix(key, "lyrics-"), taggedFormat
		}
	}
	return "", "", ""
}

func normalizedLyricsFormat(value string) string {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "LRC":
		return "LRC"
	case "PLAIN":
		return "PLAIN"
	default:
		return ""
	}
}

func splitMetadataList(value string) []string {
	seen := make(map[string]struct{})
	result := make([]string, 0)
	for _, item := range strings.Split(value, ";") {
		item = cleanText(item)
		if item == "" {
			continue
		}
		key := strings.ToLower(item)
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, item)
	}
	return result
}

func creditList(names []string, role CreditRole) []MetadataCredit {
	result := make([]MetadataCredit, 0, len(names))
	for _, name := range names {
		result = append(result, MetadataCredit{Name: name, Role: role})
	}
	return result
}

func parseNumberPair(value string) (*int, *int) {
	match := numberPairPattern.FindStringSubmatch(strings.TrimSpace(value))
	if match == nil {
		return nil, nil
	}
	number := positiveParsedInteger(match[1])
	var total *int
	if len(match) > 2 {
		total = positiveParsedInteger(match[2])
	}
	if number == nil || total == nil || *total < *number {
		return number, nil
	}
	return number, total
}

func parseReleaseDateTag(value string) *string {
	result, err := normalizeReleaseDate(strings.TrimSpace(value))
	if err != nil {
		return nil
	}
	return result
}

func parseBPM(value string) *float64 {
	parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) || parsed < 1 || parsed > 999.99 {
		return nil
	}
	parsed = math.Round(parsed*100) / 100
	return &parsed
}

func normalizeLanguage(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" || value == "xxx" || !languagePattern.MatchString(value) {
		return "und"
	}
	return value
}

func cleanText(value string) string {
	return strings.Join(strings.Fields(norm.NFKC.String(value)), " ")
}

func cleanMultiline(value string) string {
	return strings.TrimSpace(normalizeLineEndings(norm.NFC.String(value)))
}

func nullableCleanText(value string) *string {
	value = cleanText(value)
	if value == "" {
		return nil
	}
	return &value
}

func positiveIntegerPointer(value string) *int {
	return positiveParsedInteger(strings.TrimSpace(value))
}

func positiveIntPointer(value *int) *int {
	if value == nil || *value <= 0 {
		return nil
	}
	copy := *value
	return &copy
}

func positiveParsedInteger(value string) *int {
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return nil
	}
	return &parsed
}

func truthyTag(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "y":
		return true
	default:
		return false
	}
}

func safeToolError(stderr string) string {
	value := strings.TrimSpace(lineBreakPattern.ReplaceAllString(stderr, " "))
	if len(value) > 2_000 {
		value = value[:2_000]
	}
	if value == "" {
		return "Media tool failed without diagnostics"
	}
	return value
}

func defaultIfEmpty(values, fallback []string) []string {
	if len(values) == 0 {
		return fallback
	}
	return values
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func pointerValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func lyricsValue(value *MetadataLyrics) string {
	if value == nil {
		return ""
	}
	return value.Content
}

func lyricsLanguageValue(value *MetadataLyrics) string {
	if value == nil {
		return ""
	}
	return value.Language
}

func lyricsFormatValue(value *MetadataLyrics) string {
	if value == nil {
		return ""
	}
	return value.Format
}

var (
	numberPairPattern   = regexp.MustCompile(`^([0-9]+)(?:\s*/\s*([0-9]+))?$`)
	lrcTimestampPattern = regexp.MustCompile(`\[[0-9]{1,3}:[0-9]{2}(?:\.[0-9]{1,3})?\]`)
)
