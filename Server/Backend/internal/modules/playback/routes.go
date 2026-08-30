package playback

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
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
	service    *Service
	signer     *TicketSigner
	transcoder *TranscodeSessionManager
	userCtx    UserContext
}

func NewRoutes(
	service *Service,
	signer *TicketSigner,
	transcoder *TranscodeSessionManager,
	userCtx UserContext,
) (*Routes, error) {
	if service == nil || signer == nil || transcoder == nil || userCtx == nil {
		return nil, errors.New("playback routes require service, signer, transcoder, and userCtx")
	}
	return &Routes{
		service:    service,
		signer:     signer,
		transcoder: transcoder,
		userCtx:    userCtx,
	}, nil
}

func (routes *Routes) Register(router gin.IRouter) {
	router.POST("/api/v1/tracks/:id/playback", httpserver.Handle(routes.createGrant))
	router.GET("/api/v1/playback/streams/:sessionId", httpserver.Handle(routes.serveStream))
	router.HEAD("/api/v1/playback/streams/:sessionId", httpserver.Handle(routes.serveStream))
}

func (routes *Routes) createGrant(c *gin.Context) error {
	trackID := c.Param("id")
	if _, err := uuid.Parse(trackID); err != nil {
		return apperror.NotFound("Track was not found")
	}

	userID, err := routes.userCtx.CurrentUserID(c)
	if err != nil {
		return err
	}

	var input Input
	if err := c.ShouldBindJSON(&input); err != nil && c.Request.ContentLength > 0 {
		return apperror.Validation("Invalid request body")
	}
	if input.PreferredQuality == "" {
		input.PreferredQuality = QualityStandard
	}

	descriptor, err := routes.service.CreateGrant(c.Request.Context(), userID, trackID, input)
	if err != nil {
		return err
	}

	c.JSON(http.StatusOK, descriptor)
	return nil
}

func (routes *Routes) serveStream(c *gin.Context) error {
	sessionID := c.Param("sessionId")
	if _, err := uuid.Parse(sessionID); err != nil {
		return apperror.NotFound("Playback session was not found")
	}

	ticket := c.Query("ticket")
	if ticket == "" {
		return ErrInvalidTicket
	}

	claims, err := routes.signer.Verify(ticket)
	if err != nil {
		return err
	}

	if claims.SessionID != sessionID {
		return ErrInvalidTicket
	}
	if err := routes.transcoder.ValidateSession(sessionID, claims.TrackID, claims.Quality, claims.Codec); err != nil {
		return err
	}

	start := streamRangeStart(c.GetHeader("Range"))
	handle, err := routes.transcoder.OpenStreamAt(c.Request.Context(), sessionID, start)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return apperror.NotFound("Playback stream was not found or expired")
		}
		return err
	}
	defer handle.Release()
	defer handle.Reader.Close()

	_, mimeType := containerAndMime(claims.Codec)
	if mimeType != "" {
		c.Header("Content-Type", mimeType)
	}
	c.Header("ETag", fmt.Sprintf(`"%s"`, sessionID))

	if handle.Complete {
		c.Header("Accept-Ranges", "bytes")
		c.Header("Cache-Control", fmt.Sprintf("private, max-age=%d", max(1, int(routes.service.ttl/time.Second))))
		file, ok := handle.Reader.(*os.File)
		if !ok {
			return fmt.Errorf("completed playback stream is not a file")
		}
		stat, err := file.Stat()
		if err != nil {
			return fmt.Errorf("stat playback file: %w", err)
		}
		http.ServeContent(c.Writer, c.Request, file.Name(), stat.ModTime(), file)
		return nil
	}

	// A growing transcode has no final byte length yet. Do not advertise byte
	// ranges for this response; once the cache is complete, subsequent opens
	// use the regular ServeContent/range path above.
	c.Header("Accept-Ranges", "none")
	c.Header("Cache-Control", "private, no-store")
	if c.Request.Method == http.MethodHead {
		c.Status(http.StatusOK)
		return nil
	}
	c.Status(http.StatusOK)
	writer := io.Writer(c.Writer)
	if flusher, ok := c.Writer.(http.Flusher); ok {
		flusher.Flush()
		writer = flushingWriter{Writer: c.Writer, Flusher: flusher}
	}
	_, copyErr := io.Copy(writer, handle.Reader)
	if copyErr != nil && c.Request.Context().Err() == nil && !c.Writer.Written() {
		return copyErr
	}
	return nil
}

func streamRangeStart(value string) int64 {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(strings.ToLower(value), "bytes=") {
		return 0
	}
	first := strings.TrimSpace(strings.Split(strings.TrimPrefix(strings.ToLower(value), "bytes="), ",")[0])
	if first == "" || strings.HasPrefix(first, "-") {
		return 0
	}
	start, _, ok := strings.Cut(first, "-")
	if !ok {
		return 0
	}
	parsed, err := strconv.ParseInt(strings.TrimSpace(start), 10, 64)
	if err != nil || parsed < 0 {
		return 0
	}
	return parsed
}

type flushingWriter struct {
	io.Writer
	Flusher http.Flusher
}

func (writer flushingWriter) Write(value []byte) (int, error) {
	n, err := writer.Writer.Write(value)
	writer.Flusher.Flush()
	return n, err
}
