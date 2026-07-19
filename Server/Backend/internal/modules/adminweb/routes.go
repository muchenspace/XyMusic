package adminweb

import (
	"errors"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"

	"xymusic/server/internal/platform/httpserver"
	"xymusic/server/internal/shared/apperror"
)

type Assets struct {
	root string
}

func New(root string) (*Assets, error) {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	return &Assets{root: filepath.Clean(absolute)}, nil
}

func (assets *Assets) Register(engine *gin.Engine) {
	engine.GET("/", func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.Header("Location", "/admin/")
		c.Status(http.StatusFound)
	})
	redirectAdmin := func(c *gin.Context) {
		location := "/admin/"
		if c.Request.URL.RawQuery != "" {
			location += "?" + c.Request.URL.RawQuery
		}
		c.Header("Cache-Control", "no-store")
		c.Header("Location", location)
		c.Status(http.StatusPermanentRedirect)
	}
	engine.GET("/admin", redirectAdmin)
	engine.HEAD("/admin", redirectAdmin)
	engine.GET("/admin/*assetPath", assets.serve)
	engine.HEAD("/admin/*assetPath", assets.serve)
	methodNotAllowed := func(c *gin.Context) {
		c.Header("Allow", "GET, HEAD")
		c.Header("Cache-Control", "no-store")
		c.Status(http.StatusMethodNotAllowed)
	}
	for _, method := range []string{
		http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete,
		http.MethodOptions, http.MethodConnect, http.MethodTrace,
	} {
		engine.Handle(method, "/admin", methodNotAllowed)
		engine.Handle(method, "/admin/*assetPath", methodNotAllowed)
	}
}

func (assets *Assets) serve(c *gin.Context) {
	rawPath := strings.TrimPrefix(c.Param("assetPath"), "/")
	if rawPath == "" {
		rawPath = "index.html"
	}
	requested, ok := assets.safePath(rawPath)
	if !ok {
		httpserver.WriteError(c, apperror.Validation("管理后台资源路径无效，请刷新页面后重试。"))
		return
	}
	info, err := os.Stat(requested)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		httpserver.WriteError(c, err)
		return
	}
	if errors.Is(err, os.ErrNotExist) {
		if filepath.Ext(rawPath) != "" {
			httpserver.WriteError(c, apperror.NotFound("管理后台静态资源不存在，请刷新页面后重试。"))
			return
		}
		requested = filepath.Join(assets.root, "index.html")
		info, err = os.Stat(requested)
	}
	if err != nil || info.IsDir() {
		c.Header("Cache-Control", "no-store")
		c.Header("Retry-After", "5")
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"code":   "ADMIN_WEB_UNAVAILABLE",
			"detail": "管理后台尚未构建，请先完成管理后台生产构建。",
		})
		return
	}

	extension := strings.ToLower(filepath.Ext(requested))
	contentType := contentTypes[extension]
	if contentType == "" {
		contentType = mime.TypeByExtension(extension)
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	index := strings.EqualFold(filepath.Base(requested), "index.html")
	if index {
		c.Header("Cache-Control", "no-store")
	} else {
		c.Header("Cache-Control", "public, max-age=31536000, immutable")
	}
	c.Header("Content-Type", contentType)
	c.Header("Content-Security-Policy", strings.Join([]string{
		"default-src 'self'",
		"script-src 'self'",
		"style-src 'self' 'unsafe-inline'",
		"img-src 'self' data: blob: http: https:",
		"font-src 'self' data:",
		"connect-src 'self'",
		"object-src 'none'",
		"base-uri 'self'",
		"frame-ancestors 'none'",
	}, "; "))
	file, err := os.Open(requested)
	if err != nil {
		httpserver.WriteError(c, err)
		return
	}
	defer file.Close()
	http.ServeContent(c.Writer, c.Request, info.Name(), info.ModTime(), file)
}

func (assets *Assets) safePath(rawPath string) (string, bool) {
	if strings.ContainsRune(rawPath, 0) {
		return "", false
	}
	normalized := strings.ReplaceAll(rawPath, "\\", "/")
	candidate := filepath.Clean(filepath.Join(assets.root, filepath.FromSlash(normalized)))
	relative, err := filepath.Rel(assets.root, candidate)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) || strings.Contains(relative, ":") {
		return "", false
	}
	return candidate, true
}

var contentTypes = map[string]string{
	".css":         "text/css; charset=utf-8",
	".html":        "text/html; charset=utf-8",
	".ico":         "image/x-icon",
	".jpeg":        "image/jpeg",
	".jpg":         "image/jpeg",
	".js":          "text/javascript; charset=utf-8",
	".json":        "application/json; charset=utf-8",
	".png":         "image/png",
	".svg":         "image/svg+xml",
	".webmanifest": "application/manifest+json; charset=utf-8",
	".webp":        "image/webp",
	".woff2":       "font/woff2",
}
