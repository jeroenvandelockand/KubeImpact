package api

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"kubeimpact/internal/knowledge"
	"kubeimpact/internal/policy"
	clusterscan "kubeimpact/internal/scan"
	"kubeimpact/internal/sources"
	"kubeimpact/internal/storage"
)

func NewRouter(ctx context.Context) (*gin.Engine, func() error, error) {
	if err := knowledge.ValidateEmbedded(); err != nil {
		return nil, nil, err
	}
	policyConfig, err := policy.Load(os.Getenv("KUBEIMPACT_POLICY_FILE"))
	if err != nil {
		return nil, nil, err
	}
	sourceScanner, err := sources.New(sources.Config{
		Root: os.Getenv("KUBEIMPACT_SOURCE_ROOT"), AllowedGitHosts: splitCSV(os.Getenv("KUBEIMPACT_GIT_HOSTS")), SSHKnownHosts: os.Getenv("KUBEIMPACT_SSH_KNOWN_HOSTS"),
	})
	if err != nil {
		return nil, nil, err
	}

	repository, err := storage.Open(ctx, environmentOrDefault("KUBEIMPACT_DB_PATH", "data/kubeimpact.db"))
	if err != nil {
		return nil, nil, err
	}
	scanner := clusterscan.New(policyConfig, sourceScanner.Scan)
	manager := clusterscan.NewManager(repository, scanner.Run, durationFromEnvironment("KUBEIMPACT_SCAN_TIMEOUT", 60*time.Second))
	managerContext, cancelManager := context.WithCancel(ctx)
	if err := manager.Start(managerContext); err != nil {
		cancelManager()
		repository.Close()
		return nil, nil, err
	}
	server := NewServer(manager)
	var cleanupOnce sync.Once
	var cleanupErr error
	cleanup := func() error {
		cleanupOnce.Do(func() {
			cancelManager()
			manager.Wait()
			cleanupErr = repository.Close()
		})
		return cleanupErr
	}
	return NewRouterWithServer(server, os.Getenv("KUBEIMPACT_CORS_ORIGIN"), webDirectory()), cleanup, nil
}

func NewRouterWithServer(server *Server, corsOrigin, webDir string) *gin.Engine {
	router := gin.New()
	router.Use(gin.Logger(), gin.Recovery(), corsMiddleware(corsOrigin))

	v1 := router.Group("/api/v1")
	v1.GET("/health", server.Health)
	v1.POST("/scans", server.CreateScan)
	v1.POST("/scan", server.CreateScan)
	v1.GET("/scans/:id", server.GetScan)
	v1.GET("/reports", server.ListReports)
	v1.GET("/report/latest", server.LatestReport)

	registerWebUI(router, webDir)
	return router
}

func corsMiddleware(allowedOrigin string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if allowedOrigin != "" && c.GetHeader("Origin") == allowedOrigin {
			c.Header("Access-Control-Allow-Origin", allowedOrigin)
			c.Header("Vary", "Origin")
			c.Header("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			c.Header("Access-Control-Allow-Headers", "Content-Type")
		}
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

func registerWebUI(router *gin.Engine, webDir string) {
	if webDir == "" {
		return
	}
	if info, err := os.Stat(filepath.Join(webDir, "index.html")); err != nil || info.IsDir() {
		return
	}
	assets := filepath.Join(webDir, "assets")
	if info, err := os.Stat(assets); err == nil && info.IsDir() {
		router.Static("/assets", assets)
	}
	router.StaticFile("/favicon.svg", filepath.Join(webDir, "favicon.svg"))
	router.GET("/", func(c *gin.Context) { c.File(filepath.Join(webDir, "index.html")) })
}

func webDirectory() string {
	return environmentOrDefault("KUBEIMPACT_WEB_DIR", "/web")
}

func durationFromEnvironment(name string, fallback time.Duration) time.Duration {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	duration, err := time.ParseDuration(value)
	if err != nil || duration <= 0 {
		return fallback
	}
	return duration
}

func environmentOrDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func splitCSV(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			result = append(result, part)
		}
	}
	return result
}
