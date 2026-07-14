package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"

	"kubeimpact/internal/models"
)

var ErrNotFound = errors.New("scan not found")

type Repository struct {
	db *sql.DB
}

func Open(ctx context.Context, path string) (*Repository, error) {
	if path == "" {
		return nil, errors.New("database path is required")
	}
	if path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, fmt.Errorf("create database directory: %w", err)
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open SQLite database: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	repository := &Repository{db: db}
	if err := repository.initialize(ctx); err != nil {
		db.Close()
		return nil, err
	}
	if path != ":memory:" {
		if err := os.Chmod(path, 0o600); err != nil {
			db.Close()
			return nil, fmt.Errorf("secure database file: %w", err)
		}
	}
	return repository, nil
}

func (r *Repository) initialize(ctx context.Context) error {
	statements := []string{
		`PRAGMA busy_timeout = 5000`,
		`PRAGMA journal_mode = WAL`,
		`PRAGMA foreign_keys = ON`,
		`CREATE TABLE IF NOT EXISTS scans (
            id TEXT PRIMARY KEY,
            status TEXT NOT NULL,
            request_json BLOB NOT NULL,
            report_json BLOB,
            error TEXT NOT NULL DEFAULT '',
            created_at TEXT NOT NULL,
            started_at TEXT,
            completed_at TEXT
        )`,
		`CREATE INDEX IF NOT EXISTS scans_status_completed_idx ON scans(status, completed_at DESC)`,
	}
	for _, statement := range statements {
		if _, err := r.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("initialize SQLite database: %w", err)
		}
	}
	return nil
}

func (r *Repository) Close() error { return r.db.Close() }

func (r *Repository) Create(ctx context.Context, request models.ScanRequest) (*models.ScanRecord, error) {
	requestJSON, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	record := &models.ScanRecord{
		ID: uuid.NewString(), Status: models.ScanPending, Request: request, CreatedAt: time.Now().UTC(),
	}
	_, err = r.db.ExecContext(ctx, `INSERT INTO scans(id, status, request_json, created_at) VALUES(?, ?, ?, ?)`,
		record.ID, record.Status, requestJSON, formatTime(record.CreatedAt))
	if err != nil {
		return nil, fmt.Errorf("create scan record: %w", err)
	}
	return record, nil
}

func (r *Repository) MarkRunning(ctx context.Context, id string) error {
	now := time.Now().UTC()
	result, err := r.db.ExecContext(ctx, `UPDATE scans SET status = ?, started_at = ?, error = '' WHERE id = ? AND status = ?`, models.ScanRunning, formatTime(now), id, models.ScanPending)
	return updateResult(result, err, "mark scan running")
}

func (r *Repository) Complete(ctx context.Context, id string, report *models.Report) error {
	reportJSON, err := json.Marshal(report)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	result, err := r.db.ExecContext(ctx, `UPDATE scans SET status = ?, report_json = ?, completed_at = ?, error = '' WHERE id = ? AND status = ?`,
		models.ScanCompleted, reportJSON, formatTime(now), id, models.ScanRunning)
	return updateResult(result, err, "complete scan")
}

func (r *Repository) Fail(ctx context.Context, id, message string) error {
	now := time.Now().UTC()
	result, err := r.db.ExecContext(ctx, `UPDATE scans SET status = ?, error = ?, completed_at = ? WHERE id = ? AND status IN (?, ?)`,
		models.ScanFailed, message, formatTime(now), id, models.ScanPending, models.ScanRunning)
	return updateResult(result, err, "fail scan")
}

func (r *Repository) RecoverInterrupted(ctx context.Context) error {
	now := time.Now().UTC()
	_, err := r.db.ExecContext(ctx, `UPDATE scans SET status = ?, error = ?, completed_at = ? WHERE status IN (?, ?)`,
		models.ScanFailed, "scan interrupted by service restart", formatTime(now), models.ScanPending, models.ScanRunning)
	if err != nil {
		return fmt.Errorf("recover interrupted scans: %w", err)
	}
	return nil
}

func (r *Repository) Get(ctx context.Context, id string) (*models.ScanRecord, error) {
	row := r.db.QueryRowContext(ctx, selectColumns+` WHERE id = ?`, id)
	return scanRecord(row)
}

func (r *Repository) Latest(ctx context.Context) (*models.ScanRecord, error) {
	row := r.db.QueryRowContext(ctx, selectColumns+` WHERE status = ? ORDER BY completed_at DESC LIMIT 1`, models.ScanCompleted)
	return scanRecord(row)
}

func (r *Repository) ListReports(ctx context.Context, limit int) ([]models.ScanRecord, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := r.db.QueryContext(ctx, selectColumns+` WHERE status = ? ORDER BY completed_at DESC LIMIT ?`, models.ScanCompleted, limit)
	if err != nil {
		return nil, fmt.Errorf("list reports: %w", err)
	}
	defer rows.Close()

	records := make([]models.ScanRecord, 0)
	for rows.Next() {
		record, scanErr := scanRecord(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		records = append(records, *record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list reports: %w", err)
	}
	return records, nil
}

const selectColumns = `SELECT id, status, request_json, report_json, error, created_at, started_at, completed_at FROM scans`

type rowScanner interface {
	Scan(...any) error
}

func scanRecord(row rowScanner) (*models.ScanRecord, error) {
	var record models.ScanRecord
	var requestJSON []byte
	var reportJSON []byte
	var created string
	var started, completed sql.NullString
	if err := row.Scan(&record.ID, &record.Status, &requestJSON, &reportJSON, &record.Error, &created, &started, &completed); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("read scan record: %w", err)
	}
	if err := json.Unmarshal(requestJSON, &record.Request); err != nil {
		return nil, fmt.Errorf("decode stored scan request: %w", err)
	}
	if len(reportJSON) > 0 {
		record.Report = &models.Report{}
		if err := json.Unmarshal(reportJSON, record.Report); err != nil {
			return nil, fmt.Errorf("decode stored report: %w", err)
		}
	}
	var err error
	if record.CreatedAt, err = parseTime(created); err != nil {
		return nil, err
	}
	if started.Valid {
		value, parseErr := parseTime(started.String)
		if parseErr != nil {
			return nil, parseErr
		}
		record.StartedAt = &value
	}
	if completed.Valid {
		value, parseErr := parseTime(completed.String)
		if parseErr != nil {
			return nil, parseErr
		}
		record.CompletedAt = &value
	}
	return &record, nil
}

func updateResult(result sql.Result, err error, operation string) error {
	if err != nil {
		return fmt.Errorf("%s: %w", operation, err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("%s: %w", operation, err)
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func formatTime(value time.Time) string { return value.UTC().Format(time.RFC3339Nano) }

func parseTime(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse stored timestamp: %w", err)
	}
	return parsed, nil
}
