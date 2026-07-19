package control

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"xymusic/server/internal/modules/setup"
	"xymusic/server/internal/platform/httpserver"
	"xymusic/server/internal/platform/workerstatus"
)

type WorkerStatusMonitor interface {
	Status(context.Context, string) workerstatus.Snapshot
}

// HandlerOptions configures the listener-lifetime control HTTP application.
// Setup and administration routes live here so they remain reachable when no
// application runtime exists.
type HandlerOptions struct {
	Manager             *Manager
	Setup               setup.API
	RegisterAdminRoutes httpserver.RouteRegistrar
	CORS                httpserver.CORSConfig
	RequestLimits       httpserver.RequestLimits
	AllowedHosts        []string
	TrustedProxies      []string
	TraceIDGenerator    httpserver.TraceIDGenerator
	PanicHandler        httpserver.PanicHandler
	WorkerStatus        WorkerStatusMonitor
}

// NewHandler builds a control handler that is valid in first-run mode. Liveness
// remains available unconditionally; readiness is delegated to Manager's live
// dependency check, and application traffic receives 503 until a runtime is
// active.
func NewHandler(options HandlerOptions) (*gin.Engine, error) {
	if options.Manager == nil {
		return nil, errors.New("control runtime manager is required")
	}
	var readinessReport httpserver.ReadinessReporter
	if options.WorkerStatus != nil {
		readinessReport = controlReadiness{manager: options.Manager, worker: options.WorkerStatus}
	}
	return httpserver.New(httpserver.Options{
		CORS:             options.CORS,
		RequestLimits:    options.RequestLimits,
		AllowedHosts:     append([]string(nil), options.AllowedHosts...),
		Readiness:        options.Manager,
		ReadinessReport:  readinessReport,
		TrustedProxies:   append([]string(nil), options.TrustedProxies...),
		TraceIDGenerator: options.TraceIDGenerator,
		PanicHandler:     options.PanicHandler,
		RegisterRoutes: func(engine *gin.Engine) {
			if options.Setup != nil {
				setup.RegisterRoutes(engine, options.Setup)
			}
			if options.RegisterAdminRoutes != nil {
				options.RegisterAdminRoutes(engine)
			} else {
				registerUnavailableAdmin(engine)
			}
			registerRuntimeForwarding(engine, options.Manager)
		},
	})
}

type controlReadiness struct {
	manager *Manager
	worker  WorkerStatusMonitor
}

func (readiness controlReadiness) Report(ctx context.Context) (int, any) {
	runtime := readiness.manager.Status()
	active, configured := readiness.manager.ActiveConfig()
	runtimeReady := runtime.Phase == RuntimePhaseReady && configured
	if runtimeReady && readiness.manager.Check(ctx) != nil {
		runtimeReady = false
	}
	var worker *workerstatus.Snapshot
	reason := "runtime_unavailable"
	ready := false
	if runtimeReady {
		status := readiness.worker.Status(ctx, workerstatus.ConfigurationFingerprint(active))
		worker = &status
		if status.Available {
			ready = true
			reason = ""
		} else {
			reason = "worker_unavailable"
		}
	}
	status := http.StatusServiceUnavailable
	statusText := "unavailable"
	var reasonValue any = reason
	if ready {
		status = http.StatusOK
		statusText = "ready"
		reasonValue = nil
	}
	return status, gin.H{
		"status": statusText,
		"reason": reasonValue,
		"runtime": gin.H{
			"phase": runtime.Phase, "source": runtime.Source,
			"generation": runtime.Generation, "startedAt": runtime.StartedAt,
		},
		"worker": worker,
	}
}

func registerRuntimeForwarding(engine *gin.Engine, manager *Manager) {
	forward := func(c *gin.Context) {
		manager.ServeHTTP(c.Writer, c.Request)
		c.Abort()
	}
	engine.Any("/api/v1", forward)
	engine.Any("/api/v1/*path", forward)
	engine.Any("/docs", forward)
	engine.Any("/docs/*path", forward)
}

func registerUnavailableAdmin(engine *gin.Engine) {
	redirect := func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.Header("Location", "/admin/")
		c.Status(http.StatusFound)
	}
	unavailable := func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.Header("Retry-After", "5")
		if c.Request.Method == http.MethodHead {
			c.Status(http.StatusServiceUnavailable)
			return
		}
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"code":   "ADMIN_WEB_UNAVAILABLE",
			"detail": "管理后台静态资源尚未配置",
		})
	}
	engine.GET("/", redirect)
	engine.HEAD("/", redirect)
	engine.GET("/admin", redirect)
	engine.HEAD("/admin", redirect)
	engine.GET("/admin/*assetPath", unavailable)
	engine.HEAD("/admin/*assetPath", unavailable)
}
