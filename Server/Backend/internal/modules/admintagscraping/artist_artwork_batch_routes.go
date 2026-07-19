package admintagscraping

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"xymusic/server/internal/modules/adminauth"
)

func (routes *Routes) createArtistArtworkBatch(c *gin.Context) error {
	var input CreateArtistArtworkBatchInput
	shape, err := decodeContractJSON(c, &input, "items", "options")
	if err != nil {
		return err
	}
	if err := validateArtistArtworkBatchContract(input, shape); err != nil {
		return err
	}
	return routes.mutate(
		c,
		"admin.tag-scraping.artist-artwork.batch.create",
		input,
		http.StatusAccepted,
		func(actorID, _ string) (any, error) {
			return routes.artistArtworkBatches.Create(c.Request.Context(), actorID, input)
		},
	)
}

func (routes *Routes) getArtistArtworkBatch(c *gin.Context) error {
	jobID, err := contractUUID(c.Param("id"))
	if err != nil {
		return err
	}
	updatedAfter, err := contractUpdatedAfter(c)
	if err != nil {
		return err
	}
	if _, err := adminauth.RequireAdmin(c, routes.identity, false); err != nil {
		return err
	}
	job, err := routes.artistArtworkBatches.Job(c.Request.Context(), jobID, updatedAfter)
	if err != nil {
		return err
	}
	c.JSON(http.StatusOK, job)
	return nil
}

func (routes *Routes) cancelArtistArtworkBatch(c *gin.Context) error {
	jobID, err := contractUUID(c.Param("id"))
	if err != nil {
		return err
	}
	return routes.mutate(
		c,
		"admin.tag-scraping.artist-artwork.batch.cancel:"+jobID,
		map[string]any{},
		http.StatusAccepted,
		func(_, _ string) (any, error) {
			return routes.artistArtworkBatches.Cancel(c.Request.Context(), jobID)
		},
	)
}

func (routes *Routes) retryArtistArtworkBatch(c *gin.Context) error {
	jobID, err := contractUUID(c.Param("id"))
	if err != nil {
		return err
	}
	return routes.mutate(
		c,
		"admin.tag-scraping.artist-artwork.batch.retry:"+jobID,
		map[string]any{},
		http.StatusAccepted,
		func(_, _ string) (any, error) {
			return routes.artistArtworkBatches.Retry(c.Request.Context(), jobID)
		},
	)
}

func validateArtistArtworkBatchContract(
	input CreateArtistArtworkBatchInput,
	shape map[string]json.RawMessage,
) error {
	if err := requireNestedKeys(shape["options"], "sources", "overwrite", "reason"); err != nil {
		return err
	}
	return validateCreateArtistArtworkBatch(input)
}

func contractUpdatedAfter(c *gin.Context) (*time.Time, error) {
	value, exists := lastQueryValue(c.Request.URL.Query(), "updatedAfter")
	if !exists {
		return nil, nil
	}
	if !contractStringLength(value, 1, 40) {
		return nil, contractError()
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return nil, contractError()
	}
	parsed = parsed.UTC()
	return &parsed, nil
}
