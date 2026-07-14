package storage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"kubeimpact/internal/models"
)

func TestRepositoryPersistsCompletedReports(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "kubeimpact.db")
	repository, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	record, err := repository.Create(ctx, models.ScanRequest{TargetVersion: "1.36"})
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.MarkRunning(ctx, record.ID); err != nil {
		t.Fatal(err)
	}
	report := &models.Report{ScanID: record.ID, TargetVersion: "1.36", Findings: []models.Finding{}, UpgradeImpact: []models.UpgradeImpact{}}
	if err := repository.Complete(ctx, record.ID, report); err != nil {
		t.Fatal(err)
	}
	if err := repository.Close(); err != nil {
		t.Fatal(err)
	}

	repository, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	latest, err := repository.Latest(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if latest.ID != record.ID || latest.Status != models.ScanCompleted || latest.Report == nil || latest.Report.TargetVersion != "1.36" {
		t.Fatalf("latest = %#v", latest)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("database permissions = %o", info.Mode().Perm())
	}
}

func TestRecoverInterruptedScans(t *testing.T) {
	ctx := context.Background()
	repository, err := Open(ctx, filepath.Join(t.TempDir(), "kubeimpact.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	pending, _ := repository.Create(ctx, models.ScanRequest{TargetVersion: "1.36"})
	running, _ := repository.Create(ctx, models.ScanRequest{TargetVersion: "1.36"})
	if err := repository.MarkRunning(ctx, running.ID); err != nil {
		t.Fatal(err)
	}
	if err := repository.RecoverInterrupted(ctx); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{pending.ID, running.ID} {
		record, getErr := repository.Get(ctx, id)
		if getErr != nil || record.Status != models.ScanFailed || record.Error == "" {
			t.Fatalf("record %s = %#v, %v", id, record, getErr)
		}
	}
	if _, err := repository.Get(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get(missing) error = %v", err)
	}
}
