package admintagscraping

var searchableArtistSources = []Source{SourceQMusic, SourceNetease}

var defaultSmartArtistSources = []Source{SourceQMusic, SourceNetease}

type ArtistSearchInput struct {
	Source  Source   `json:"source"`
	Query   string   `json:"query"`
	Sources []Source `json:"sources,omitempty"`
}

type ArtistCandidate struct {
	Source   Source   `json:"source"`
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	ImageURL string   `json:"imageUrl"`
	Aliases  []string `json:"aliases"`
	Score    float64  `json:"score"`
}

type ArtistArtworkApplyInput struct {
	ExpectedVersion int             `json:"expectedVersion"`
	Candidate       ArtistCandidate `json:"candidate"`
	Overwrite       bool            `json:"overwrite"`
	Reason          string          `json:"reason"`
}

type ArtistArtworkApplyResult struct {
	Applied bool `json:"applied"`
	Version int  `json:"version"`
}
