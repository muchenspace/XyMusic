package playback

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
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
	router.GET("/api/v1/playback/streams/:sessionId/index.m3u8", httpserver.Handle(routes.servePlaylist))
	router.HEAD("/api/v1/playback/streams/:sessionId/index.m3u8", httpserver.Handle(routes.servePlaylist))
	router.GET("/api/v1/playback/streams/:sessionId/hls/:name", httpserver.Handle(routes.serveSegment))
	router.HEAD("/api/v1/playback/streams/:sessionId/hls/:name", httpserver.Handle(routes.serveSegment))
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
	claims, err := routes.verifyStream(c)
	if err != nil {
		return err
	}

	handle, err := routes.transcoder.OpenCompletedStream(c.Request.Context(), claims.SessionID)
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
	c.Header("Accept-Ranges", "bytes")
	c.Header("Cache-Control", fmt.Sprintf("private, max-age=%d", max(1, int(routes.service.ttl/time.Second))))
	c.Header("ETag", fmt.Sprintf(`"%s"`, claims.SessionID))
	file, ok := handle.Reader.(*os.File)
	if !ok || !handle.Complete {
		return fmt.Errorf("completed playback stream is not a file")
	}
	stat, err := file.Stat()
	if err != nil {
		return fmt.Errorf("stat playback file: %w", err)
	}
	http.ServeContent(c.Writer, c.Request, file.Name(), stat.ModTime(), file)
	return nil
}

func (routes *Routes) servePlaylist(c *gin.Context) error {
	claims, err := routes.verifyStream(c)
	if err != nil {
		return err
	}
	handle, err := routes.transcoder.OpenHLS(c.Request.Context(), claims.SessionID)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return apperror.NotFound("Playback playlist was not found or expired")
		}
		return err
	}
	defer handle.Release()
	cacheKey, err := routes.transcoder.sessionCacheKey(claims.SessionID)
	if err != nil {
		return err
	}
	rewritten, err := rewriteHLSPlaylist(handle.Content, claims.SessionID, c.Query("ticket"), cacheKey)
	if err != nil {
		return err
	}
	c.Header("Content-Type", "application/vnd.apple.mpegurl")
	c.Header("Cache-Control", "private, no-cache, no-store, must-revalidate")
	c.Header("Pragma", "no-cache")
	c.Header("ETag", fmt.Sprintf(`"%s-playlist"`, claims.SessionID))
	c.Header("Content-Length", fmt.Sprintf("%d", len(rewritten)))
	if c.Request.Method == http.MethodHead {
		c.Status(http.StatusOK)
		return nil
	}
	c.Data(http.StatusOK, "application/vnd.apple.mpegurl", rewritten)
	return nil
}

func (routes *Routes) serveSegment(c *gin.Context) error {
	claims, err := routes.verifyStream(c)
	if err != nil {
		return err
	}
	expectedCacheKey, err := routes.transcoder.sessionCacheKey(claims.SessionID)
	if err != nil {
		return err
	}
	if c.Query("cacheKey") != expectedCacheKey {
		return ErrInvalidTicket
	}
	name := c.Param("name")
	handle, err := routes.transcoder.OpenHLSSegment(c.Request.Context(), claims.SessionID, name)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return apperror.NotFound("Playback segment was not found or expired")
		}
		return err
	}
	defer handle.Release()
	contentType := "audio/mp4"
	c.Header("Content-Type", contentType)
	c.Header("Accept-Ranges", "bytes")
	c.Header("Cache-Control", fmt.Sprintf("private, max-age=%d", max(1, int(routes.service.ttl/time.Second))))
	c.Header("ETag", fmt.Sprintf(`"%s-%s"`, claims.SessionID, name))
	file, ok := handle.Reader.(io.ReadSeeker)
	if !ok {
		return fmt.Errorf("HLS segment is not seekable")
	}
	http.ServeContent(c.Writer, c.Request, name, time.Time{}, file)
	return nil
}

func (routes *Routes) verifyStream(c *gin.Context) (*TicketClaims, error) {
	sessionID := c.Param("sessionId")
	if _, err := uuid.Parse(sessionID); err != nil {
		return nil, apperror.NotFound("Playback session was not found")
	}
	ticket := c.Query("ticket")
	if ticket == "" {
		return nil, ErrInvalidTicket
	}
	claims, err := routes.signer.Verify(ticket)
	if err != nil {
		return nil, err
	}
	if claims.SessionID != sessionID {
		return nil, ErrInvalidTicket
	}
	if err := routes.transcoder.ValidateSession(sessionID, claims.UserID, claims.TrackID, claims.Quality, claims.Codec); err != nil {
		return nil, err
	}
	return claims, nil
}

func rewriteHLSPlaylist(content []byte, sessionID, ticket, cacheKey string) ([]byte, error) {
	if strings.TrimSpace(ticket) == "" || strings.TrimSpace(sessionID) == "" {
		return nil, ErrInvalidTicket
	}
	query := "ticket=" + url.QueryEscape(ticket)
	if cacheKey != "" {
		query += "&cacheKey=" + url.QueryEscape(cacheKey)
	}
	segmentURL := func(name string) string {
		return "hls/" + url.PathEscape(name) + "?" + query
	}
	lines := strings.SplitAfter(string(content), "\n")
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#EXT-X-MAP:") {
			lines[index] = rewriteHLSURIAttribute(line, segmentURL)
			continue
		}
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		lineEnd := ""
		lineBody := line
		if strings.HasSuffix(lineBody, "\n") {
			lineBody = strings.TrimSuffix(lineBody, "\n")
			lineEnd = "\n"
		}
		if strings.HasSuffix(lineBody, "\r") {
			lineBody = strings.TrimSuffix(lineBody, "\r")
			lineEnd = "\r" + lineEnd
		}
		name := strings.TrimSpace(lineBody)
		if name == "" || strings.Contains(name, "://") || strings.ContainsAny(name, `/\\`) {
			return nil, fmt.Errorf("invalid HLS playlist URI")
		}
		lines[index] = segmentURL(name) + lineEnd
	}
	return []byte(strings.Join(lines, "")), nil
}

func rewriteHLSURIAttribute(line string, segmentURL func(string) string) string {
	const prefix = `URI="`
	start := strings.Index(line, prefix)
	if start < 0 {
		return line
	}
	valueStart := start + len(prefix)
	valueEnd := strings.Index(line[valueStart:], `"`)
	if valueEnd < 0 {
		return line
	}
	valueEnd += valueStart
	name := line[valueStart:valueEnd]
	if name == "" || strings.ContainsAny(name, `/\\`) || strings.Contains(name, "://") {
		return line
	}
	return line[:valueStart] + segmentURL(name) + line[valueEnd:]
}
