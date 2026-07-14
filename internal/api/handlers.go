package api

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"kubeimpact/internal/knowledge"
	"kubeimpact/internal/models"
)

type ScanFunc func(context.Context, string) (*models.Report, error)

type Server struct {
	reports     *reportStore
	scan        ScanFunc
	scanTimeout time.Duration
	scanLock    sync.Mutex
}

func NewServer(scan ScanFunc, scanTimeout time.Duration) *Server {
	return &Server{
		reports:     newReportStore(),
		scan:        scan,
		scanTimeout: scanTimeout,
	}
}

func (s *Server) Health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (s *Server) Scan(c *gin.Context) {
	targetVersion := knowledge.NormalizeVersion(c.DefaultQuery("targetVersion", "1.36"))
	if !knowledge.IsSupportedVersion(targetVersion) {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":             "unsupported targetVersion",
			"supportedVersions": knowledge.SupportedVersions(),
		})
		return
	}
	if !s.scanLock.TryLock() {
		c.JSON(http.StatusConflict, gin.H{"error": "a cluster scan is already in progress"})
		return
	}
	defer s.scanLock.Unlock()

	ctx, cancel := context.WithTimeout(c.Request.Context(), s.scanTimeout)
	defer cancel()

	report, err := s.scan(ctx, targetVersion)
	if err != nil {
		log.Printf("cluster scan failed: %v", err)
		status := http.StatusInternalServerError
		message := "cluster scan failed"
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			status = http.StatusGatewayTimeout
			message = "cluster scan timed out"
		} else if strings.Contains(err.Error(), "must be newer than current version") {
			status = http.StatusUnprocessableEntity
			message = err.Error()
		}
		c.JSON(status, gin.H{"error": message})
		return
	}
	if report == nil {
		log.Print("cluster scan returned a nil report")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "cluster scan failed"})
		return
	}

	s.reports.Set(report)
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, report)
}

func (s *Server) LatestReport(c *gin.Context) {
	report, ok := s.reports.Get()
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "no completed scan is available"})
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, report)
}
