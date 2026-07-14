package scan

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"kubeimpact/internal/models"
	"kubeimpact/internal/storage"
)

func TestManagerPersistsJobsAndComparesReports(t *testing.T) {
	repository, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "kubeimpact.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()

	var calls atomic.Int32
	runner := func(context.Context, models.ScanRequest) (*models.Report, error) {
		report := &models.Report{
			PolicyProfile: "restricted", Findings: []models.Finding{}, UpgradeImpact: []models.UpgradeImpact{}, Warnings: []string{}, Sources: []models.SourceResult{}, Suppressions: []models.Suppression{},
		}
		if calls.Add(1) == 1 {
			report.Findings = append(report.Findings, models.Finding{ID: "RULE", Fingerprint: "fingerprint", Severity: models.High, Kind: "Deployment", Name: "api", Message: "risk"})
		}
		return report, nil
	}
	manager := NewManager(repository, runner, time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := manager.Start(ctx); err != nil {
		t.Fatal(err)
	}
	request := models.ScanRequest{TargetVersion: "1.36"}

	first, err := manager.Enqueue(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	first = waitForStatus(t, manager, first.ID, models.ScanCompleted)
	if first.Report.Comparison.New != 1 || first.Report.Findings[0].Change != models.ChangeNew {
		t.Fatalf("first comparison = %#v", first.Report.Comparison)
	}

	second, err := manager.Enqueue(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	second = waitForStatus(t, manager, second.ID, models.ScanCompleted)
	if second.Report.Comparison.PreviousScanID != first.ID || second.Report.Comparison.Resolved != 1 || len(second.Report.Comparison.ResolvedItems) != 1 {
		t.Fatalf("second comparison = %#v", second.Report.Comparison)
	}
	reports, err := manager.ListReports(context.Background(), 20)
	if err != nil || len(reports) != 2 || reports[0].ID != second.ID {
		t.Fatalf("reports = %#v, %v", reports, err)
	}
}

func TestManagerTimesOutScan(t *testing.T) {
	repository, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "kubeimpact.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	manager := NewManager(repository, func(ctx context.Context, _ models.ScanRequest) (*models.Report, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}, 10*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := manager.Start(ctx); err != nil {
		t.Fatal(err)
	}
	record, err := manager.Enqueue(context.Background(), models.ScanRequest{TargetVersion: "1.36"})
	if err != nil {
		t.Fatal(err)
	}
	record = waitForStatus(t, manager, record.ID, models.ScanFailed)
	if record.Error != "scan timed out" {
		t.Fatalf("Error = %q", record.Error)
	}
}

func TestManagerFailsQueuedScansDuringShutdown(t *testing.T) {
	repository, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "kubeimpact.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	manager := NewManager(repository, func(ctx context.Context, _ models.ScanRequest) (*models.Report, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}, time.Minute)
	ctx, cancel := context.WithCancel(context.Background())
	if err := manager.Start(ctx); err != nil {
		t.Fatal(err)
	}
	first, err := manager.Enqueue(context.Background(), models.ScanRequest{TargetVersion: "1.36"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := manager.Enqueue(context.Background(), models.ScanRequest{TargetVersion: "1.36"})
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	manager.Wait()
	for _, id := range []string{first.ID, second.ID} {
		record, getErr := repository.Get(context.Background(), id)
		if getErr != nil || record.Status != models.ScanFailed || record.Error == "" {
			t.Fatalf("shutdown record %s = %#v, %v", id, record, getErr)
		}
	}
	if _, err := manager.Enqueue(context.Background(), models.ScanRequest{TargetVersion: "1.36"}); err == nil {
		t.Fatal("Enqueue after shutdown returned no error")
	}
}

func TestComparableRequestsTreatsNilAndEmptySourcesEqually(t *testing.T) {
	left := models.ScanRequest{TargetVersion: "v1.36.2"}
	right := models.ScanRequest{TargetVersion: "1.36", Sources: []models.SourceSpec{}}
	if !comparableRequests(left, right) {
		t.Fatal("semantically identical cluster requests were not comparable")
	}
}

func TestManagerRejectsFullQueueWithoutBlocking(t *testing.T) {
	repository, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "kubeimpact.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	started := make(chan struct{})
	manager := NewManager(repository, func(ctx context.Context, _ models.ScanRequest) (*models.Report, error) {
		select {
		case started <- struct{}{}:
		default:
		}
		<-ctx.Done()
		return nil, ctx.Err()
	}, time.Minute)
	manager.queue = make(chan string, 1)
	ctx, cancel := context.WithCancel(context.Background())
	if err := manager.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Enqueue(context.Background(), models.ScanRequest{TargetVersion: "1.36"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first scan did not start")
	}
	if _, err := manager.Enqueue(context.Background(), models.ScanRequest{TargetVersion: "1.36"}); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Enqueue(context.Background(), models.ScanRequest{TargetVersion: "1.36"}); err == nil {
		t.Fatal("full queue accepted another scan")
	}
	cancel()
	manager.Wait()
}

func TestComparisonDoesNotCallSuppressedFindingResolved(t *testing.T) {
	previous := &models.ScanRecord{ID: "previous", Report: &models.Report{Findings: []models.Finding{{
		ID: "RULE", Fingerprint: "same-fingerprint", Severity: models.High, Kind: "Deployment", Name: "api", Message: "risk",
	}}}}
	current := &models.Report{
		Findings: []models.Finding{}, UpgradeImpact: []models.UpgradeImpact{},
		Suppressions: []models.Suppression{{RuleID: "RULE", Fingerprint: "same-fingerprint", Reason: "reviewed exception"}},
	}
	ApplyComparison(current, previous)
	if current.Comparison.Resolved != 0 || len(current.Comparison.ResolvedItems) != 0 {
		t.Fatalf("comparison = %#v", current.Comparison)
	}
}

func waitForStatus(t *testing.T, manager *Manager, id string, wanted models.ScanStatus) *models.ScanRecord {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		record, err := manager.Get(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if record.Status == wanted {
			return record
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("scan %s did not reach %s", id, wanted)
	return nil
}
