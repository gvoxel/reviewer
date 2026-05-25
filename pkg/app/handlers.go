package app

import (
	"context"
	"fmt"
	"html"
	"io/fs"
	"log/slog"
	"net/http"
	_ "net/http/pprof"
	"net/url"
	"os"
	"strings"

	"reviewsrv/frontend"
	"reviewsrv/pkg/rest"
	"reviewsrv/pkg/slack"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/vmkteam/appkit"
	"github.com/vmkteam/rpcgen/v2"
	"github.com/vmkteam/rpcgen/v2/typescript"
	"github.com/vmkteam/zenrpc/v2"
)

// runHTTPServer is a function that starts http listener using labstack/echo.
func (a *App) runHTTPServer(ctx context.Context, host string, port int) error {
	listenAddress := fmt.Sprintf("%s:%d", host, port)
	addr := "http://" + listenAddress
	a.Print(ctx, "starting http listener", "url", addr, "smdbox", addr+"/v1/rpc/doc/")

	return a.echo.Start(listenAddress)
}

// registerHandlers register echo handlers.
func (a *App) registerHandlers() {
	a.echo.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins: []string{"*"},
		AllowMethods: []string{echo.GET, echo.PUT, echo.POST, echo.DELETE},
		AllowHeaders: []string{"Authorization", "Authorization2", "Origin", "X-Requested-With", "Content-Type", "Accept", "Platform", "Version"},
	}), middleware.BodyLimit("2M"))

	lg := middleware.RequestLoggerWithConfig(middleware.RequestLoggerConfig{
		LogStatus:    true,
		LogURI:       true,
		LogError:     true,
		HandleError:  true,
		LogLatency:   true,
		LogRemoteIP:  true,
		LogRequestID: true,
		LogUserAgent: true,
		LogValuesFunc: func(c echo.Context, v middleware.RequestLoggerValues) error {
			attrs := []slog.Attr{
				slog.String("ip", v.RemoteIP),
				slog.String("uri", v.URI),
				slog.Int("status", v.Status),
				slog.String("userAgent", v.UserAgent),
				slog.String("duration", v.Latency.String()),
				slog.String("xRequestId", v.RequestID),
			}

			if v.Error == nil {
				a.Log().LogAttrs(context.Background(), slog.LevelInfo, "http request", attrs...)
			} else {
				a.Log().LogAttrs(context.Background(), slog.LevelError, "http request error", append(attrs, slog.String("err", v.Error.Error()))...)
			}
			return nil
		},
	})

	h := rest.NewHandler(a.db, slack.NewNotifier(a.Logger), a.cfg.Server.BaseURL)

	a.echo.GET("/v1/prompt/:projectKey/", h.GetPrompt, lg)
	a.echo.POST("/v1/upload/:projectKey/", h.CreateReview, lg)
	a.echo.POST("/v1/upload/:projectKey/:reviewId/:reviewType/", h.UploadReviewFile, lg)
	a.echo.GET("/v1/rpc/review-fix-:id", h.ReviewFixMarkdown, lg)
}

// registerDownloadHandlers serves static files from cfg.Server.DownloadDir at
// /download/ — reviewctl release binaries (and SHA256SUMS) that the RS AI
// Launcher fetches by fixed URL. The directory root renders a custom listing
// that shows the build version; individual files are served as-is. No-op when
// DownloadDir is empty.
func (a *App) registerDownloadHandlers() {
	dir := a.cfg.Server.DownloadDir
	if dir == "" {
		return
	}
	fileServer := http.StripPrefix("/download/", http.FileServer(http.Dir(dir)))
	a.echo.GET("/download/*", func(c echo.Context) error {
		if rel := c.Param("*"); rel == "" || rel == "/" {
			return a.renderDownloadIndex(c, dir)
		}
		fileServer.ServeHTTP(c.Response(), c.Request())
		return nil
	})
	a.echo.GET("/download", func(c echo.Context) error {
		return c.Redirect(http.StatusMovedPermanently, "/download/")
	})
}

// renderDownloadIndex renders an HTML listing of dir that shows the build
// version alongside the available files. Replaces the default http.FileServer
// directory autoindex so the version is visible.
func (a *App) renderDownloadIndex(c echo.Context, dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return c.String(http.StatusInternalServerError, "download directory unavailable")
	}

	var b strings.Builder
	b.WriteString("<!doctype html><html lang=\"en\"><head><meta charset=\"utf-8\">")
	b.WriteString("<title>reviewctl downloads</title></head><body>")
	fmt.Fprintf(&b, "<h1>reviewctl downloads</h1><p>build version: <strong>%s</strong></p><ul>",
		html.EscapeString(a.version))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		size := ""
		if info, err := e.Info(); err == nil {
			size = " — " + humanizeBytes(info.Size())
		}
		fmt.Fprintf(&b, "<li><a href=\"%s\">%s</a>%s</li>",
			url.PathEscape(name), html.EscapeString(name), html.EscapeString(size))
	}
	b.WriteString("</ul></body></html>")

	return c.HTML(http.StatusOK, b.String())
}

// humanizeBytes formats a byte count as a short human-readable string.
func humanizeBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit; x /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

// registerDebugHandlers adds /debug/pprof handlers into a.echo instance.
func (a *App) registerDebugHandlers() {
	dbg := a.echo.Group("/debug")

	// add pprof integration
	dbg.Any("/pprof/*", appkit.PprofHandler)

	// add healthcheck
	a.echo.GET("/status", func(c echo.Context) error {
		// test postgresql connection
		err := a.db.Ping(c.Request().Context())
		if err != nil {
			a.Error(c.Request().Context(), "failed to check db connection", "err", err)
			return c.String(http.StatusInternalServerError, "DB error")
		}
		return c.String(http.StatusOK, "OK")
	})

	// show all routes in devel mode
	if a.cfg.Server.IsDevel {
		a.echo.GET("/", appkit.RenderRoutes(a.appName, a.echo))
	} else {
		a.echo.GET("/", func(c echo.Context) error {
			return c.Redirect(http.StatusFound, "/reviews/")
		})
	}
}

// registerAPIHandlers registers main rpc server.
func (a *App) registerAPIHandlers() {
	gen := rpcgen.FromSMD(a.srv.SMD())

	a.echo.Any("/v1/rpc/", appkit.EchoHandler(appkit.XRequestID(a.srv)))
	a.echo.Any("/v1/rpc/doc/", appkit.EchoHandlerFunc(zenrpc.SMDBoxHandler))
	a.echo.Any("/v1/rpc/openrpc.json", appkit.EchoHandlerFunc(rpcgen.Handler(gen.OpenRPC("reviewsrv", "http://localhost:8075/v1/rpc"))))
	a.echo.Any("/v1/rpc/api.ts", appkit.EchoHandlerFunc(rpcgen.Handler(gen.TSClient(nil))))
}

// registerSPAHandlers serves an embedded SPA at the given prefix.
// indexFile is the name of the HTML entry point inside distFS (e.g. "index.html", "vt.html").
func (a *App) registerSPAHandlers(distFS fs.FS, prefix, indexFile string) {
	fileServer := http.FileServer(http.FS(distFS))

	// serve static assets
	a.echo.GET(prefix+"assets/*", echo.WrapHandler(http.StripPrefix(prefix, fileServer)))
	a.echo.GET(prefix+"favicon.svg", echo.WrapHandler(http.StripPrefix(prefix, fileServer)))

	// SPA fallback
	a.echo.GET(prefix+"*", func(c echo.Context) error {
		index, err := fs.ReadFile(distFS, indexFile)
		if err != nil {
			return c.String(http.StatusInternalServerError, "frontend not found")
		}
		return c.HTMLBlob(http.StatusOK, index)
	})
	a.echo.GET(prefix[:len(prefix)-1], func(c echo.Context) error {
		return c.Redirect(http.StatusMovedPermanently, prefix)
	})
}

// registerFrontendHandlers serves the embedded SPA frontend at /reviews/.
func (a *App) registerFrontendHandlers() error {
	distFS, err := fs.Sub(frontend.DistFS, "dist")
	if err != nil {
		return fmt.Errorf("frontend fs: %w", err)
	}
	a.registerSPAHandlers(distFS, "/reviews/", "index.html")
	return nil
}

// registerVTApiHandlers registers vt rpc server.
func (a *App) registerVTApiHandlers() {
	gen := rpcgen.FromSMD(a.vtsrv.SMD())
	tsSettings := typescript.Settings{ExcludedNamespace: []string{}, WithClasses: true}

	a.echo.Any("/v1/vt/", appkit.EchoHandler(appkit.XRequestID(a.vtsrv)))
	a.echo.Any("/v1/vt/doc/", appkit.EchoHandlerFunc(zenrpc.SMDBoxHandler))
	a.echo.Any("/v1/vt/api.ts", appkit.EchoHandlerFunc(rpcgen.Handler(gen.TSCustomClient(tsSettings))))
}

// registerVTFrontendHandlers serves the embedded VT admin SPA at /vt/.
func (a *App) registerVTFrontendHandlers() error {
	distFS, err := fs.Sub(frontend.DistVTFS, "dist-vt")
	if err != nil {
		return fmt.Errorf("vt frontend fs: %w", err)
	}
	a.registerSPAHandlers(distFS, "/vt/", "vt.html")
	return nil
}
