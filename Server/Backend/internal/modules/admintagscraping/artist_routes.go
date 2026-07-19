package admintagscraping

import (
	"encoding/json"
	"math"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"xymusic/server/internal/modules/adminauth"
)

func (routes *Routes) searchArtists(c *gin.Context) error {
	var input ArtistSearchInput
	shape, err := decodeContractJSON(c, &input, "source", "query")
	if err != nil {
		return err
	}
	if err := validateArtistSearchInput(input, shape); err != nil {
		return err
	}
	if _, err := adminauth.RequireAdmin(c, routes.identity, true); err != nil {
		return err
	}
	result, err := routes.scraping.SearchArtists(c.Request.Context(), input)
	if err != nil {
		return err
	}
	c.JSON(http.StatusOK, result)
	return nil
}

func (routes *Routes) applyArtistArtwork(c *gin.Context) error {
	artistID, err := contractUUID(c.Param("id"))
	if err != nil {
		return err
	}
	var input ArtistArtworkApplyInput
	shape, err := decodeContractJSON(c, &input, "expectedVersion", "candidate", "overwrite", "reason")
	if err != nil {
		return err
	}
	if err := validateArtistArtworkApplyInput(input, shape); err != nil {
		return err
	}
	return routes.mutate(
		c,
		"admin.tag-scraping.artist-artwork.apply:"+artistID,
		input,
		http.StatusOK,
		func(actorID, traceID string) (any, error) {
			return routes.scraping.ApplyArtistArtwork(
				c.Request.Context(), actorID, traceID, artistID, input,
			)
		},
	)
}

func validateArtistSearchInput(input ArtistSearchInput, shape map[string]json.RawMessage) error {
	if input.Source != SourceSmart && !isArtistSearchableSource(input.Source) {
		return contractError()
	}
	query := strings.TrimSpace(input.Query)
	if !contractStringLength(query, 1, 300) ||
		hasExplicitNull(shape, "sources") {
		return contractError()
	}
	_, sourcesProvided := shape["sources"]
	if input.Source != SourceSmart {
		if sourcesProvided {
			return contractError()
		}
		return nil
	}
	if !sourcesProvided {
		return nil
	}
	if len(input.Sources) < 1 || len(input.Sources) > len(searchableArtistSources) ||
		!uniqueSources(input.Sources) {
		return contractError()
	}
	for _, source := range input.Sources {
		if !isArtistSearchableSource(source) {
			return contractError()
		}
	}
	return nil
}

func validateArtistArtworkApplyInput(
	input ArtistArtworkApplyInput,
	shape map[string]json.RawMessage,
) error {
	reason := strings.TrimSpace(input.Reason)
	if input.ExpectedVersion < 1 || input.ExpectedVersion == math.MaxInt ||
		!contractStringLength(reason, 2, 500) {
		return contractError()
	}
	if err := requireNestedKeys(
		shape["candidate"], "source", "id", "name", "imageUrl", "aliases", "score",
	); err != nil {
		return err
	}
	if err := validateArtistCandidate(input.Candidate); err != nil {
		return contractError()
	}
	return nil
}
