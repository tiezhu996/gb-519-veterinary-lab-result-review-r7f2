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
	StatusCounts(context.Context) (map[string]int64, error)
}

type criticalDispositionService struct {
	repository repository.CriticalDispositionRepository
}

func NewCriticalDispositionService(repo repository.CriticalDispositionRepository) CriticalDispositionService {
	return &criticalDispositionService{repository: repo}
}

func (s *criticalDispositionService) List(ctx context.Context, query dto.PageQuery) (repository.Page[model.CriticalDisposition], error) {
	return s.repository.List(ctx, query)
}

func (s *criticalDispositionService) Get(ctx context.Context, id uint) (model.CriticalDisposition, error) {
	return s.repository.Get(ctx, id)
}

// Confirm 校验确认人角色与异人约束后，以条件更新保证重复/并发确认仅一次成功。
func (s *criticalDispositionService) Confirm(ctx context.Context, id uint, input dto.ConfirmCriticalDisposition, actor, role, requestID string) (model.CriticalDisposition, error) {
	if role != model.RoleReviewer && role != model.RoleAdmin {
		return model.CriticalDisposition{}, ErrReviewRequired
	}
	recipient := strings.TrimSpace(input.Recipient)
	measure := strings.TrimSpace(input.Measure)
	if recipient == "" || measure == "" {
		return model.CriticalDisposition{}, ErrInvalidInput
	}
	current, err := s.repository.Get(ctx, id)
	if err != nil {
		return model.CriticalDisposition{}, err
	}
	if current.Status != model.DispositionStatePending {
		return model.CriticalDisposition{}, ErrDispositionClosed
	}
	if actor == current.RunOperator {
		return model.CriticalDisposition{}, ErrRunOperatorBlocked
	}
	return s.repository.Confirm(ctx, id, input.ExpectedVersion, recipient, measure, actor, requestID)
}

func (s *criticalDispositionService) StatusCounts(ctx context.Context) (map[string]int64, error) {
	return s.repository.CountByStatus(ctx)
}
