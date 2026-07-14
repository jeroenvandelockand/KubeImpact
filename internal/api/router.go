package api

import (
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"

	"kubeimpact/internal/knowledge"
	clusterscan "kubeimpact/internal/scan"
)

func NewRouter() (*gin.Engine, error) {
	if err := knowledge.ValidateEmbedded(); err != nil {
		return nil, err
	}

	scanner := clusterscan.New()
	timeout := durationFromEnvironment("KUBEIMPACT_SCAN_TIMEOUT", 60*time.Second)
	server := NewServer(scanner.Run, timeout)
	return NewRouterWithServer(server, os.Getenv("KUBEIMPACT_CORS_ORIGIN"), webDirectory()), nil
}

func NewRouterWithServer(server *Server, corsOrigin, webDir string) *gin.Engine {
	router := gin.New()
	router.Use(gin.Logger(), gin.Recovery(), corsMiddleware(corsOrigin))

	v1 := router.Group("/api/v1")
	v1.GET("/health", server.Health)
	v1.POST("/scan", server.Scan)
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
	router.GET("/", func(c *gin.Context) {
		c.File(filepath.Join(webDir, "index.html"))
	})
}

func webDirectory() string {
	if configured := os.Getenv("KUBEIMPACT_WEB_DIR"); configured != "" {
		return configured
	}
	return "/web"
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
