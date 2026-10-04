package playback

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"xymusic/server/internal/platform/httpserver"
	"xymusic/server/internal/shared/apperror"
)

type UserContext interface {
	CurrentUserID(c *gin.Context) (string, error)
}

type Routes struct {
	service *Service
	signer  *TicketSigner
	userCtx UserContext
	opener  AssetOpener
}

func NewRoutes(
	service *Service,
	signer *TicketSigner,
	userCtx UserContext,
	opener AssetOpener,
) (*Routes, error) {
	if service == nil || signer == nil || userCtx == nil {
		return nil, errors.New("playback routes require service, signer, and userCtx")
	}
	if opener == nil {
		opener = NewOSAssetOpener()
	}
	return &Routes{
		service: service,
		signer:  signer,
		userCtx: userCtx,
		opener:  opener,
	}, nil
}

func (routes *Routes) Register(router gin.IRouter) {
	router.POST("/api/v1/tracks/:id/playback", httpserver.Handle(routes.createGrant))
	router.GET("/api/v1/playback/streams/:trackId", httpserver.Handle(routes.serveStream))
	router.HEAD("/api/v1/playback/streams/:trackId", httpserver.Handle(routes.serveStream))
}

func (routes *Routes) createGrant(c *gin.Context) error {
	trackID := c.Param("id")
	if _, err := uuid.Parse(trackID); err != nil {
		return apperror.NotFound("Track was not found")
	}

	var request map[string]json.RawMessage
	if err := httpserver.DecodeJSON(c, &request); err != nil {
		return err
	}
	if len(request) != 0 {
		return apperror.Validation("Playback grant requests do not accept quality or codec selectors")
	}

	userID, err := routes.userCtx.CurrentUserID(c)
	if err != nil {
		return err
	}

	descriptor, err := routes.service.CreateGrant(c.Request.Context(), userID, trackID)
	if err != nil {
		return err
	}

	c.JSON(http.StatusOK, descriptor)
	return nil
}

func (routes *Routes) serveStream(c *gin.Context) error {
	claims, err := routes.verifyStream(c)
	if err != nil {
		return err
	}

	source, err := routes.service.ResolveSource(c.Request.Context(), claims.TrackID)
	if err != nil {
		return err
	}

	file, _, modTime, err := routes.opener.Open(source.SourcePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return apperror.NotFound("Audio file was not found")
		}
		var openError *AssetOpenError
		if errors.As(err, &openError) && openError.Operation == "stat" {
			return fmt.Errorf("stat audio file: %w", openError.Err)
		}
		return fmt.Errorf("open audio file: %w", err)
	}
	defer file.Close()

	format := sourceFormatForPath(source.SourcePath)
	if format.mimeType != "" {
		c.Header("Content-Type", format.mimeType)
	}
	c.Header("Accept-Ranges", "bytes")
	c.Header("Cache-Control", fmt.Sprintf("private, max-age=%d", max(1, int(routes.service.ttl/time.Second))))
	if source.ChecksumSHA256 != "" {
		c.Header("ETag", fmt.Sprintf(`"%s"`, source.ChecksumSHA256))
	}
	http.ServeContent(c.Writer, c.Request, filepath.Base(source.SourcePath), modTime, file)
	return nil
}

func (routes *Routes) verifyStream(c *gin.Context) (*TicketClaims, error) {
	trackID := c.Param("trackId")
	if _, err := uuid.Parse(trackID); err != nil {
		return nil, apperror.NotFound("Playback stream was not found")
	}
	ticket := c.Query("ticket")
	if ticket == "" {
		return nil, ErrInvalidTicket
	}
	claims, err := routes.signer.Verify(ticket)
	if err != nil {
		return nil, err
	}
	if claims.TrackID != trackID {
		return nil, ErrInvalidTicket
	}
	return claims, nil
}
