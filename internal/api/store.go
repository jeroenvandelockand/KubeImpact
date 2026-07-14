package api

import (
	"sync"

	"kubeimpact/internal/models"
)

type reportStore struct {
	mu     sync.RWMutex
	latest *models.Report
}

func newReportStore() *reportStore {
	return &reportStore{}
}

func (s *reportStore) Get() (*models.Report, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.latest == nil {
		return nil, false
	}
	return s.latest, true
}

func (s *reportStore) Set(report *models.Report) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.latest = report
}
