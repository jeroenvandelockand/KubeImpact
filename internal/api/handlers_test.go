package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"kubeimpact/internal/models"
)

func TestLatestReportDoesNotTriggerScan(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var calls atomic.Int32
	server := NewServer(func(context.Context, string) (*models.Report, error) {
		calls.Add(1)
		return &models.Report{}, nil
	}, time.Second)
	router := NewRouterWithServer(server, "", "")

	response := request(router, http.MethodGet, "/api/v1/report/latest")
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
	}
	if calls.Load() != 0 {
		t.Fatalf("scan calls = %d, want 0", calls.Load())
	}
}

func TestSuccessfulScanBecomesLatestReport(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := NewServer(func(_ context.Context, target string) (*models.Report, error) {
		return &models.Report{ClusterVersion: "v1.35.2", TargetVersion: target, Findings: []models.Finding{}, UpgradeImpact: []models.UpgradeImpact{}, Warnings: []string{}}, nil
	}, time.Second)
	router := NewRouterWithServer(server, "", "")

	scanResponse := request(router, http.MethodPost, "/api/v1/scan?targetVersion=v1.36.1")
	if scanResponse.Code != http.StatusOK {
		t.Fatalf("scan status = %d, body = %s", scanResponse.Code, scanResponse.Body.String())
	}
	latestResponse := request(router, http.MethodGet, "/api/v1/report/latest")
	if latestResponse.Code != http.StatusOK {
		t.Fatalf("latest status = %d, body = %s", latestResponse.Code, latestResponse.Body.String())
	}
	var report models.Report
	if err := json.Unmarshal(latestResponse.Body.Bytes(), &report); err != nil {
		t.Fatalf("decode latest report: %v", err)
	}
	if report.TargetVersion != "1.36" {
		t.Fatalf("TargetVersion = %q", report.TargetVersion)
	}
}

func TestFailedScanKeepsPreviousReport(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var calls atomic.Int32
	server := NewServer(func(_ context.Context, target string) (*models.Report, error) {
		if calls.Add(1) == 1 {
			return &models.Report{ClusterVersion: "previous", TargetVersion: target}, nil
		}
		return nil, errors.New("sensitive internal failure")
	}, time.Second)
	router := NewRouterWithServer(server, "", "")

	if response := request(router, http.MethodPost, "/api/v1/scan?targetVersion=1.36"); response.Code != http.StatusOK {
		t.Fatalf("first scan status = %d", response.Code)
	}
	failed := request(router, http.MethodPost, "/api/v1/scan?targetVersion=1.36")
	if failed.Code != http.StatusInternalServerError || strings.Contains(failed.Body.String(), "sensitive internal failure") {
		t.Fatalf("failed scan = %d %s", failed.Code, failed.Body.String())
	}

	latest := request(router, http.MethodGet, "/api/v1/report/latest")
	var report models.Report
	if err := json.Unmarshal(latest.Body.Bytes(), &report); err != nil {
		t.Fatalf("decode latest report: %v", err)
	}
	if report.ClusterVersion != "previous" {
		t.Fatalf("latest ClusterVersion = %q", report.ClusterVersion)
	}
}

func TestScanValidatesTargetBeforeCallingScanner(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var calls atomic.Int32
	server := NewServer(func(context.Context, string) (*models.Report, error) {
		calls.Add(1)
		return &models.Report{}, nil
	}, time.Second)
	response := request(NewRouterWithServer(server, "", ""), http.MethodPost, "/api/v1/scan?targetVersion=9.99")
	if response.Code != http.StatusBadRequest || calls.Load() != 0 {
		t.Fatalf("status/calls = %d/%d", response.Code, calls.Load())
	}
}

func TestConcurrentScanIsRejected(t *testing.T) {
	gin.SetMode(gin.TestMode)
	started := make(chan struct{})
	release := make(chan struct{})
	server := NewServer(func(context.Context, string) (*models.Report, error) {
		close(started)
		<-release
		return &models.Report{}, nil
	}, time.Second)
	router := NewRouterWithServer(server, "", "")

	done := make(chan *httptest.ResponseRecorder)
	go func() {
		done <- request(router, http.MethodPost, "/api/v1/scan?targetVersion=1.36")
	}()
	<-started
	conflict := request(router, http.MethodPost, "/api/v1/scan?targetVersion=1.36")
	if conflict.Code != http.StatusConflict {
		t.Fatalf("concurrent scan status = %d", conflict.Code)
	}
	close(release)
	if response := <-done; response.Code != http.StatusOK {
		t.Fatalf("first scan status = %d", response.Code)
	}
}

type httpHandler interface {
	ServeHTTP(http.ResponseWriter, *http.Request)
}

func request(handler httpHandler, method, path string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(method, path, nil))
	return response
}
