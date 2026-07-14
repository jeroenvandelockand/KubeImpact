package scan

import (
	"context"
	"errors"
	"fmt"
	"log"
	"reflect"
	"strings"
	"sync"
	"time"

	"kubeimpact/internal/knowledge"
	"kubeimpact/internal/models"
	"kubeimpact/internal/storage"
)

type Repository interface {
	Create(context.Context, models.ScanRequest) (*models.ScanRecord, error)
	MarkRunning(context.Context, string) error
	Complete(context.Context, string, *models.Report) error
	Fail(context.Context, string, string) error
	RecoverInterrupted(context.Context) error
	Get(context.Context, string) (*models.ScanRecord, error)
	Latest(context.Context) (*models.ScanRecord, error)
	ListReports(context.Context, int) ([]models.ScanRecord, error)
}

type Runner func(context.Context, models.ScanRequest) (*models.Report, error)

type Manager struct {
	repository Repository
	run        Runner
	timeout    time.Duration
	queue      chan string
	startOnce  sync.Once
	contextMu  sync.RWMutex
	ctx        context.Context
	done       chan struct{}
}

func NewManager(repository Repository, run Runner, timeout time.Duration) *Manager {
	return &Manager{repository: repository, run: run, timeout: timeout, queue: make(chan string, 100), done: make(chan struct{})}
}

func (m *Manager) Start(ctx context.Context) error {
	var startErr error
	m.startOnce.Do(func() {
		if err := m.repository.RecoverInterrupted(ctx); err != nil {
			startErr = err
			return
		}
		m.contextMu.Lock()
		m.ctx = ctx
		m.contextMu.Unlock()
		go m.worker(ctx)
	})
	return startErr
}

func (m *Manager) Enqueue(ctx context.Context, request models.ScanRequest) (*models.ScanRecord, error) {
	managerCtx := m.managerContext()
	if managerCtx == nil {
		return nil, errors.New("scan manager is not started")
	}
	if err := managerCtx.Err(); err != nil {
		return nil, errors.New("scan manager is shutting down")
	}
	record, err := m.repository.Create(ctx, request)
	if err != nil {
		return nil, err
	}
	select {
	case m.queue <- record.ID:
		select {
		case <-managerCtx.Done():
			m.fail(record.ID, errors.New("scan was not queued: scan manager is shutting down"))
			return nil, errors.New("scan manager is shutting down")
		default:
			return record, nil
		}
	case <-ctx.Done():
		failCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = m.repository.Fail(failCtx, record.ID, "scan was not queued: "+ctx.Err().Error())
		return nil, ctx.Err()
	case <-managerCtx.Done():
		m.fail(record.ID, errors.New("scan was not queued: scan manager is shutting down"))
		return nil, errors.New("scan manager is shutting down")
	default:
		m.fail(record.ID, errors.New("scan was not queued: scan queue is full"))
		return nil, errors.New("scan queue is full")
	}
}

func (m *Manager) managerContext() context.Context {
	m.contextMu.RLock()
	defer m.contextMu.RUnlock()
	return m.ctx
}

func (m *Manager) Wait() { <-m.done }

func (m *Manager) Get(ctx context.Context, id string) (*models.ScanRecord, error) {
	return m.repository.Get(ctx, id)
}

func (m *Manager) Latest(ctx context.Context) (*models.ScanRecord, error) {
	return m.repository.Latest(ctx)
}

func (m *Manager) ListReports(ctx context.Context, limit int) ([]models.ScanRecord, error) {
	return m.repository.ListReports(ctx, limit)
}

func (m *Manager) worker(ctx context.Context) {
	defer close(m.done)
	defer func() {
		recoveryCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := m.repository.RecoverInterrupted(recoveryCtx); err != nil {
			log.Printf("mark interrupted scans failed during shutdown: %v", err)
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case id := <-m.queue:
			m.execute(ctx, id)
		}
	}
}

func (m *Manager) execute(parent context.Context, id string) {
	updateCtx, cancelUpdate := context.WithTimeout(context.Background(), 5*time.Second)
	if err := m.repository.MarkRunning(updateCtx, id); err != nil {
		cancelUpdate()
		log.Printf("mark scan %s running: %v", id, err)
		return
	}
	cancelUpdate()

	record, err := m.repository.Get(parent, id)
	if err != nil {
		m.fail(id, fmt.Errorf("load queued scan: %w", err))
		return
	}
	ctx, cancel := context.WithTimeout(parent, m.timeout)
	report, err := m.run(ctx, record.Request)
	cancel()
	if err != nil {
		m.fail(id, err)
		return
	}
	if report == nil {
		m.fail(id, errors.New("scan returned no report"))
		return
	}

	report.ScanID = id
	previous := m.previousComparable(record.Request, report.PolicyProfile, report.PolicyFingerprint)
	ApplyComparison(report, previous)
	completeCtx, cancelComplete := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelComplete()
	if err := m.repository.Complete(completeCtx, id, report); err != nil {
		log.Printf("persist completed scan %s: %v", id, err)
	}
}

func (m *Manager) previousComparable(request models.ScanRequest, profile, policyFingerprint string) *models.ScanRecord {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	records, err := m.repository.ListReports(ctx, 100)
	if err != nil {
		log.Printf("load previous reports for comparison: %v", err)
		return nil
	}
	for i := range records {
		if comparableRequests(request, records[i].Request) && records[i].Report != nil && records[i].Report.PolicyProfile == profile && records[i].Report.PolicyFingerprint == policyFingerprint {
			return &records[i]
		}
	}
	return nil
}

func comparableRequests(left, right models.ScanRequest) bool {
	if knowledge.NormalizeVersion(left.TargetVersion) != knowledge.NormalizeVersion(right.TargetVersion) || left.ClusterEnabled() != right.ClusterEnabled() || !sourcesEqual(left.Sources, right.Sources) {
		return false
	}
	return left.ClusterEnabled() || knowledge.NormalizeVersion(left.CurrentVersion) == knowledge.NormalizeVersion(right.CurrentVersion)
}

func sourcesEqual(left, right []models.SourceSpec) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if !reflect.DeepEqual(left[i], right[i]) {
			return false
		}
	}
	return true
}

func (m *Manager) fail(id string, scanErr error) {
	log.Printf("scan %s failed: %v", id, scanErr)
	message := strings.TrimSpace(scanErr.Error())
	if errors.Is(scanErr, context.DeadlineExceeded) {
		message = "scan timed out"
	}
	if len(message) > 1000 {
		message = message[:1000] + "…"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := m.repository.Fail(ctx, id, message); err != nil && !errors.Is(err, storage.ErrNotFound) {
		log.Printf("persist failed scan %s: %v", id, err)
	}
}
