package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"kubeimpact/internal/models"
	"kubeimpact/internal/storage"
)

type fakeManager struct {
	enqueued int
	records  map[string]*models.ScanRecord
	reports  []models.ScanRecord
}

func (f *fakeManager) Enqueue(_ context.Context, request models.ScanRequest) (*models.ScanRecord, error) {
	f.enqueued++
	record := &models.ScanRecord{ID: "scan-1", Status: models.ScanPending, Request: request, CreatedAt: time.Now().UTC()}
	if f.records == nil {
		f.records = map[string]*models.ScanRecord{}
	}
	f.records[record.ID] = record
	return record, nil
}

func (f *fakeManager) Get(_ context.Context, id string) (*models.ScanRecord, error) {
	if record := f.records[id]; record != nil {
		return record, nil
	}
	return nil, storage.ErrNotFound
}

func (f *fakeManager) Latest(context.Context) (*models.ScanRecord, error) {
	if len(f.reports) == 0 {
		return nil, storage.ErrNotFound
	}
	return &f.reports[0], nil
}

func (f *fakeManager) ListReports(context.Context, int) ([]models.ScanRecord, error) {
	return f.reports, nil
}

func TestLatestReportDoesNotTriggerScan(t *testing.T) {
	gin.SetMode(gin.TestMode)
	manager := &fakeManager{}
	response := request(NewRouterWithServer(NewServer(manager), "", ""), http.MethodGet, "/api/v1/report/latest", nil)
	if response.Code != http.StatusNotFound || manager.enqueued != 0 {
		t.Fatalf("status/enqueued = %d/%d", response.Code, manager.enqueued)
	}
}

func TestCreateScanQueuesPersistentJob(t *testing.T) {
	gin.SetMode(gin.TestMode)
	manager := &fakeManager{}
	body := bytes.NewBufferString(`{"targetVersion":"v1.36.2","includeCluster":true}`)
	response := request(NewRouterWithServer(NewServer(manager), "", ""), http.MethodPost, "/api/v1/scans", body)
	if response.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if response.Header().Get("Location") != "/api/v1/scans/scan-1" || manager.enqueued != 1 {
		t.Fatalf("location/enqueued = %q/%d", response.Header().Get("Location"), manager.enqueued)
	}
	var record models.ScanRecord
	if err := json.Unmarshal(response.Body.Bytes(), &record); err != nil || record.Request.TargetVersion != "1.36" {
		t.Fatalf("record = %#v, error = %v", record, err)
	}
}

func TestCreateScanSupportsEmptyBodyAndManifestOnlyRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)
	manager := &fakeManager{}
	router := NewRouterWithServer(NewServer(manager), "", "")

	if response := request(router, http.MethodPost, "/api/v1/scans", nil); response.Code != http.StatusAccepted {
		t.Fatalf("empty body status = %d, body = %s", response.Code, response.Body.String())
	}
	body := bytes.NewBufferString(`{"targetVersion":"1.36","currentVersion":"v1.35.4","includeCluster":false,"sources":[{"type":"directory","path":"manifests"}]}`)
	response := request(router, http.MethodPost, "/api/v1/scans", body)
	if response.Code != http.StatusAccepted {
		t.Fatalf("manifest-only status = %d, body = %s", response.Code, response.Body.String())
	}
	var record models.ScanRecord
	if err := json.Unmarshal(response.Body.Bytes(), &record); err != nil || record.Request.CurrentVersion != "1.35" {
		t.Fatalf("record = %#v, error = %v", record, err)
	}
}

func TestCreateScanValidatesRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	manager := &fakeManager{}
	router := NewRouterWithServer(NewServer(manager), "", "")
	for _, body := range []string{
		`{"targetVersion":"9.99"}`,
		`{"targetVersion":"1.36","includeCluster":false}`,
		`{"targetVersion":"1.36","includeCluster":false,"sources":[{"type":"directory","path":"manifests"}]}`,
		`{"targetVersion":"1.36","sources":[{"type":"unknown"}]}`,
		`{"targetVersion":"1.36","sources":[{"type":"directory"}]}`,
		`{"targetVersion":"1.36","sources":[{"type":"helm","path":"chart","releaseName":"--post-renderer"}]}`,
		`{"targetVersion":"1.36","sources":[{"type":"git","url":"https://token:secret@github.com/example/repo.git"}]}`,
		`{"targetVersion":"1.36","sources":[{"type":"directory","path":"manifests"},{"type":"directory","path":"manifests"}]}`,
		`{"targetVersion":"1.36"} {"targetVersion":"1.35"}`,
		`{"targetVersion":"1.36","unexpected":true}`,
		`null`,
	} {
		response := request(router, http.MethodPost, "/api/v1/scans", bytes.NewBufferString(body))
		if response.Code != http.StatusBadRequest {
			t.Errorf("body %s status = %d", body, response.Code)
		}
	}
	if manager.enqueued != 0 {
		t.Fatalf("enqueued = %d", manager.enqueued)
	}
}

func TestGetScanAndReportHistory(t *testing.T) {
	gin.SetMode(gin.TestMode)
	report := &models.Report{ScanID: "completed"}
	manager := &fakeManager{
		records: map[string]*models.ScanRecord{"running": {ID: "running", Status: models.ScanRunning}},
		reports: []models.ScanRecord{{ID: "completed", Status: models.ScanCompleted, Report: report}},
	}
	router := NewRouterWithServer(NewServer(manager), "", "")
	if response := request(router, http.MethodGet, "/api/v1/scans/running", nil); response.Code != http.StatusOK {
		t.Fatalf("scan status = %d", response.Code)
	}
	if response := request(router, http.MethodGet, "/api/v1/reports", nil); response.Code != http.StatusOK {
		t.Fatalf("reports status = %d", response.Code)
	}
	if response := request(router, http.MethodGet, "/api/v1/report/latest", nil); response.Code != http.StatusOK {
		t.Fatalf("latest status = %d", response.Code)
	}
	if response := request(router, http.MethodGet, "/api/v1/scans/missing", nil); response.Code != http.StatusNotFound {
		t.Fatalf("missing status = %d", response.Code)
	}
}

type httpHandler interface {
	ServeHTTP(http.ResponseWriter, *http.Request)
}

func request(handler httpHandler, method, path string, body *bytes.Buffer) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	var requestBody anyReader
	if body != nil {
		requestBody = body
	}
	req := httptest.NewRequest(method, path, requestBody)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	handler.ServeHTTP(response, req)
	return response
}

type anyReader interface {
	Read([]byte) (int, error)
}
