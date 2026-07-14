package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"kubeimpact/internal/knowledge"
	"kubeimpact/internal/models"
	"kubeimpact/internal/sources"
	"kubeimpact/internal/storage"
)

const maxSourcesPerScan = 20

type ScanManager interface {
	Enqueue(context.Context, models.ScanRequest) (*models.ScanRecord, error)
	Get(context.Context, string) (*models.ScanRecord, error)
	Latest(context.Context) (*models.ScanRecord, error)
	ListReports(context.Context, int) ([]models.ScanRecord, error)
}

type Server struct {
	scans ScanManager
}

func NewServer(scans ScanManager) *Server { return &Server{scans: scans} }

func (s *Server) Health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (s *Server) CreateScan(c *gin.Context) {
	request := models.ScanRequest{}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	var decoded *models.ScanRequest
	decodeErr := decoder.Decode(&decoded)
	if decodeErr != nil && !errors.Is(decodeErr, io.EOF) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid scan request: " + decodeErr.Error()})
		return
	}
	if decoded != nil {
		request = *decoded
	} else if !errors.Is(decodeErr, io.EOF) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "scan request must be a JSON object"})
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "scan request must contain one JSON object"})
		return
	}
	if queryTarget := c.Query("targetVersion"); request.TargetVersion == "" && queryTarget != "" {
		request.TargetVersion = queryTarget
	}
	if request.TargetVersion == "" {
		request.TargetVersion = "1.36"
	}
	request.TargetVersion = knowledge.NormalizeVersion(request.TargetVersion)
	if !knowledge.IsSupportedVersion(request.TargetVersion) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported targetVersion", "supportedVersions": knowledge.SupportedVersions()})
		return
	}
	if !request.ClusterEnabled() && len(request.Sources) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "scan must include the cluster or at least one source"})
		return
	}
	if !request.ClusterEnabled() {
		if request.CurrentVersion == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "currentVersion is required for a manifest-only scan"})
			return
		}
		request.CurrentVersion = knowledge.NormalizeVersion(request.CurrentVersion)
		if _, err := knowledge.LoadForUpgrade(request.CurrentVersion, request.TargetVersion); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid manifest-only upgrade path: " + err.Error()})
			return
		}
	}
	if len(request.Sources) > maxSourcesPerScan {
		c.JSON(http.StatusBadRequest, gin.H{"error": "a scan may contain at most 20 sources"})
		return
	}
	seenSources := make(map[string]struct{}, len(request.Sources))
	for index, source := range request.Sources {
		if err := sources.ValidateSpec(source); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid source " + strconv.Itoa(index+1) + ": " + err.Error()})
			return
		}
		encoded, _ := json.Marshal(source)
		key := string(encoded)
		if _, duplicate := seenSources[key]; duplicate {
			c.JSON(http.StatusBadRequest, gin.H{"error": "duplicate source at position " + strconv.Itoa(index+1)})
			return
		}
		seenSources[key] = struct{}{}
	}

	record, err := s.scans.Enqueue(c.Request.Context(), request)
	if err != nil {
		log.Printf("queue scan: %v", err)
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "scan could not be queued"})
		return
	}
	c.Header("Location", "/api/v1/scans/"+record.ID)
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusAccepted, record)
}

func (s *Server) GetScan(c *gin.Context) {
	record, err := s.scans.Get(c.Request.Context(), c.Param("id"))
	if errors.Is(err, storage.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "scan not found"})
		return
	}
	if err != nil {
		log.Printf("get scan: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "scan could not be read"})
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, record)
}

func (s *Server) LatestReport(c *gin.Context) {
	record, err := s.scans.Latest(c.Request.Context())
	if errors.Is(err, storage.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "no completed scan is available"})
		return
	}
	if err != nil {
		log.Printf("get latest report: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "latest report could not be read"})
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, record.Report)
}

func (s *Server) ListReports(c *gin.Context) {
	limit := 20
	if value := c.Query("limit"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 1 || parsed > 100 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "limit must be between 1 and 100"})
			return
		}
		limit = parsed
	}
	records, err := s.scans.ListReports(c.Request.Context(), limit)
	if err != nil {
		log.Printf("list reports: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "reports could not be read"})
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"reports": records})
}
