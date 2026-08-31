package httpserver

import (
	"compress/gzip"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// GzipJSON compresses JSON responses when the client advertises gzip. Admin
// list pages can contain thousands of rows, so this reduces transfer time and
// keeps large-library pagination usable without changing the response schema.
// Streaming/audio/SSE responses are deliberately left untouched.
func GzipJSON() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !acceptsGzip(c.Request) {
			c.Next()
			return
		}
		writer := &gzipJSONWriter{ResponseWriter: c.Writer, request: c.Request}
		c.Writer = writer
		c.Next()
		writer.close()
	}
}

type gzipJSONWriter struct {
	gin.ResponseWriter
	request       *http.Request
	gzipWriter    *gzip.Writer
	compressed    bool
	wrote         bool
	status        int
	headerWritten bool
}

func (writer *gzipJSONWriter) WriteHeader(status int) {
	if writer.wrote {
		return
	}
	writer.wrote = true
	writer.status = status
	// Gin's JSON renderer may set Content-Type immediately before writing the
	// body rather than before WriteHeader. Delay a header with no content type
	// until Write so large JSON responses are still compressed reliably.
	if writer.shouldCompress(status) {
		writer.start(status)
		return
	}
	if writer.Header().Get("Content-Type") != "" || status != http.StatusOK {
		writer.ResponseWriter.WriteHeader(status)
		writer.headerWritten = true
	}
}

func (writer *gzipJSONWriter) Write(value []byte) (int, error) {
	if !writer.wrote {
		writer.WriteHeader(http.StatusOK)
	}
	if !writer.compressed && !writer.headerWritten && writer.shouldCompress(writer.status) {
		writer.start(writer.status)
	}
	if !writer.compressed && !writer.headerWritten {
		writer.ResponseWriter.WriteHeader(writer.status)
		writer.headerWritten = true
	}
	if writer.compressed {
		return writer.gzipWriter.Write(value)
	}
	return writer.ResponseWriter.Write(value)
}

func (writer *gzipJSONWriter) WriteString(value string) (int, error) {
	return writer.Write([]byte(value))
}

func (writer *gzipJSONWriter) Flush() {
	if !writer.wrote {
		writer.WriteHeader(http.StatusOK)
	}
	if !writer.compressed && !writer.headerWritten && writer.shouldCompress(writer.status) {
		writer.start(writer.status)
	}
	if !writer.compressed && !writer.headerWritten {
		writer.ResponseWriter.WriteHeader(writer.status)
		writer.headerWritten = true
	}
	if writer.compressed {
		_ = writer.gzipWriter.Flush()
	}
	writer.ResponseWriter.Flush()
}

func (writer *gzipJSONWriter) close() {
	if writer.gzipWriter != nil {
		_ = writer.gzipWriter.Close()
		writer.gzipWriter = nil
	}
	if writer.wrote && !writer.headerWritten {
		writer.ResponseWriter.WriteHeader(writer.status)
		writer.headerWritten = true
	}
}

func (writer *gzipJSONWriter) start(status int) {
	header := writer.Header()
	header.Del("Content-Length")
	header.Set("Content-Encoding", "gzip")
	appendVary(header, "Accept-Encoding")
	writer.gzipWriter = gzip.NewWriter(writer.ResponseWriter)
	writer.compressed = true
	writer.ResponseWriter.WriteHeader(status)
	writer.headerWritten = true
}

func (writer *gzipJSONWriter) shouldCompress(status int) bool {
	if writer.request == nil || writer.request.Method == http.MethodHead || status == http.StatusNoContent || status == http.StatusNotModified {
		return false
	}
	header := writer.Header()
	if header.Get("Content-Encoding") != "" || strings.EqualFold(header.Get("Cache-Control"), "no-transform") {
		return false
	}
	contentType := strings.ToLower(strings.TrimSpace(strings.SplitN(header.Get("Content-Type"), ";", 2)[0]))
	return contentType == "application/json" || contentType == "application/problem+json"
}

func acceptsGzip(request *http.Request) bool {
	if request == nil {
		return false
	}
	for _, item := range strings.Split(strings.ToLower(request.Header.Get("Accept-Encoding")), ",") {
		parts := strings.Split(item, ";")
		if strings.TrimSpace(parts[0]) != "gzip" {
			continue
		}
		quality := 1.0
		for _, parameter := range parts[1:] {
			key, value, ok := strings.Cut(strings.TrimSpace(parameter), "=")
			if ok && strings.EqualFold(key, "q") {
				if parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64); err == nil {
					quality = parsed
				}
			}
		}
		return quality > 0
	}
	return false
}

func appendVary(header http.Header, value string) {
	for _, existing := range header.Values("Vary") {
		for _, item := range strings.Split(existing, ",") {
			if strings.EqualFold(strings.TrimSpace(item), value) {
				return
			}
		}
	}
	header.Add("Vary", value)
}
