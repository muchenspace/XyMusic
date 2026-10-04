package mediafile

import (
	"xymusic/server/internal/shared/lyrics"
)

type CreditRole string

const (
	CreditPrimary  CreditRole = "PRIMARY"
	CreditFeatured CreditRole = "FEATURED"
	CreditComposer CreditRole = "COMPOSER"
	CreditLyricist CreditRole = "LYRICIST"
	CreditProducer CreditRole = "PRODUCER"
)

type EditableField string

const (
	FieldTitle        EditableField = "title"
	FieldCredits      EditableField = "credits"
	FieldAlbumArtists EditableField = "albumArtists"
	FieldAlbum        EditableField = "album"
	FieldReleaseDate  EditableField = "releaseDate"
	FieldTrackNumber  EditableField = "trackNumber"
	FieldTrackTotal   EditableField = "trackTotal"
	FieldDiscNumber   EditableField = "discNumber"
	FieldDiscTotal    EditableField = "discTotal"
	FieldGenres       EditableField = "genres"
	FieldBPM          EditableField = "bpm"
	FieldISRC         EditableField = "isrc"
	FieldComment      EditableField = "comment"
	FieldCopyright    EditableField = "copyright"
	FieldLyrics       EditableField = "lyrics"
)

var EditableFields = []EditableField{
	FieldTitle, FieldCredits, FieldAlbumArtists, FieldAlbum, FieldReleaseDate,
	FieldTrackNumber, FieldTrackTotal, FieldDiscNumber, FieldDiscTotal,
	FieldGenres, FieldBPM, FieldISRC, FieldComment, FieldCopyright, FieldLyrics,
}

type MetadataCredit struct {
	Name string     `json:"name"`
	Role CreditRole `json:"role"`
}

type MetadataLyrics struct {
	Content  string        `json:"content"`
	Format   string        `json:"format"`
	Language string        `json:"language"`
	Timing   lyrics.Timing `json:"timing"`
}

type MetadataSnapshot struct {
	Title        string           `json:"title"`
	Credits      []MetadataCredit `json:"credits"`
	AlbumArtists []string         `json:"albumArtists"`
	Album        *string          `json:"album"`
	ReleaseDate  *string          `json:"releaseDate"`
	TrackNumber  *int             `json:"trackNumber"`
	TrackTotal   *int             `json:"trackTotal"`
	DiscNumber   *int             `json:"discNumber"`
	DiscTotal    *int             `json:"discTotal"`
	Genres       []string         `json:"genres"`
	BPM          *float64         `json:"bpm"`
	ISRC         *string          `json:"isrc"`
	Comment      *string          `json:"comment"`
	Copyright    *string          `json:"copyright"`
	Lyrics       *MetadataLyrics  `json:"lyrics"`
	HasArtwork   bool             `json:"hasArtwork"`
}

type MetadataOverrides map[string]any
