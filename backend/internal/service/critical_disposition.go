package service

import (
	"context"
	"strings"

	"github.com/blueship581/veterinary-lab-result-review/backend/internal/dto"
	"github.com/blueship581/veterinary-lab-result-review/backend/internal/model"
	"github.com/blueship581/veterinary-lab-result-review/backend/internal/repository"
)

type CriticalDispositionService interface {
	List(context.Context, dto.PageQuery) (repository.Page[model.CriticalDisposition], error)
	Get(context.Context, uint) (model.CriticalDisposition, error)
	Confirm(context.Context, uint, dto.ConfirmCriticalDisposition, string, string, string) (model.CriticalDisposition, error)
}

type criticalDispositionService struct {
	repository repository.CriticalDispositionRepository
	assays     repository.AssayRunRepository
}

func NewCriticalDispositionService(repo repository.CriticalDispositionRepository, assays repository.AssayRunRepository) CriticalDispositionService {
	return &criticalDispositionService{repository: repo, assays: assays}
}

func (s *criticalDispositionService) List(ctx context.Context, query dto.PageQuery) (repository.Page[model.CriticalDisposition], error) {
	return s.repository.List(ctx, query)
}

func (s *criticalDispositionService) Get(ctx context.Context, id uint) (model.CriticalDisposition, error) {
	return s.repository.Get(ctx, id)
}

// Confirm enforces the gate rules: only reviewer/admin may confirm, the
// confirmer must differ from the assay run operator, the target run must still
// be validated, and the receiving target plus handling action are mandatory.
// Atomic conditional persistence makes repeated/concurrent confirmation win
// once.
func (s *criticalDispositionService) Confirm(ctx context.Context, id uint, input dto.ConfirmCriticalDisposition, actor, role, requestID string) (model.CriticalDisposition, error) {
	if !isSignoffReviewerRole(role) {
		return model.CriticalDisposition{}, ErrReviewRequired
	}
	current, err := s.repository.Get(ctx, id)
	if err != nil {
		return model.CriticalDisposition{}, err
	}
	if current.Status != model.CriticalDispositionInitialStatus {
		return model.CriticalDisposition{}, ErrDispositionState
	}
	run, err := s.assays.Get(ctx, current.AssayRunID)
	if err != nil {
		return model.CriticalDisposition{}, err
	}
	if run.Status != "validated" {
		return model.CriticalDisposition{}, ErrAssayInvalid
	}
	runOperator := current.RunOperator
	if run.OperatedBy != "" {
		runOperator = run.OperatedBy
	}
	if runOperator != "" && actor == runOperator {
		return model.CriticalDisposition{}, ErrRunOperator
	}
	receiveTarget := strings.TrimSpace(input.ReceiveTarget)
	action := strings.TrimSpace(input.DispositionAction)
	reason := strings.TrimSpace(input.Reason)
	if receiveTarget == "" || action == "" {
		return model.CriticalDisposition{}, ErrInvalidInput
	}
	return s.repository.Confirm(ctx, id, input.ExpectedVersion, actor, receiveTarget, action, reason, requestID)
}
