package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/blueship581/veterinary-lab-result-review/backend/internal/constants"
	"github.com/blueship581/veterinary-lab-result-review/backend/internal/dto"
	"github.com/blueship581/veterinary-lab-result-review/backend/internal/model"
	"github.com/blueship581/veterinary-lab-result-review/backend/internal/repository"
	"gorm.io/gorm"
)

type AssayRunService interface {
	List(context.Context, dto.PageQuery) (repository.Page[model.AssayRun], error)
	Get(context.Context, uint) (model.AssayRun, error)
	Create(context.Context, dto.CreateAssayRun, string, string) (model.AssayRun, error)
	Update(context.Context, uint, dto.UpdateAssayRun, string, string) (model.AssayRun, error)
	Transition(context.Context, uint, dto.TransitionRequest, string, string) (model.AssayRun, error)
	Delete(context.Context, uint, string, string) error
	StatusCounts(context.Context) (map[string]int64, error)
}

type assayRunService struct {
	repository  repository.AssayRunRepository
	disposition repository.CriticalDispositionRepository
	security    SecurityService
}

func NewAssayRunService(repo repository.AssayRunRepository, disposition repository.CriticalDispositionRepository, security SecurityService) AssayRunService {
	return &assayRunService{repository: repo, disposition: disposition, security: security}
}

func (s *assayRunService) List(ctx context.Context, query dto.PageQuery) (repository.Page[model.AssayRun], error) {
	return s.repository.List(ctx, query)
}

func (s *assayRunService) Get(ctx context.Context, id uint) (model.AssayRun, error) {
	return s.repository.Get(ctx, id)
}

func (s *assayRunService) Create(ctx context.Context, input dto.CreateAssayRun, actor, requestID string) (model.AssayRun, error) {
	if err := validateAssayRunBusinessFields(input.Code, input.Name, input.Facility, input.Owner); err != nil {
		return model.AssayRun{}, err
	}
	item := model.AssayRun{
		BaseModel: model.BaseModel{
			Code: strings.ToUpper(strings.TrimSpace(input.Code)), Name: strings.TrimSpace(input.Name),
			Status: model.AssayRunInitialStatus, Version: 1, Description: strings.TrimSpace(input.Description),
		},
		Facility: strings.TrimSpace(input.Facility), Owner: strings.TrimSpace(input.Owner),
		Category: strings.TrimSpace(input.Category), RiskLevel: input.RiskLevel,
		MetricValue: input.MetricValue, MetricUnit: strings.TrimSpace(input.MetricUnit),
		EffectiveAt: input.EffectiveAt.UTC(), Evidence: strings.TrimSpace(input.Evidence),
		RelatedCode: strings.ToUpper(strings.TrimSpace(input.RelatedCode)), OperatedBy: actor,
	}
	if err := s.repository.Create(ctx, &item); err != nil {
		return model.AssayRun{}, fmt.Errorf("create 检测运行: %w", err)
	}
	_ = s.security.Audit(ctx, actor, requestID, "create", "AssayRun", item.ID, "", item.Status, "created 检测运行")
	return item, nil
}

func (s *assayRunService) Update(ctx context.Context, id uint, input dto.UpdateAssayRun, actor, requestID string) (model.AssayRun, error) {
	current, err := s.repository.Get(ctx, id)
	if err != nil {
		return model.AssayRun{}, err
	}
	if err := validateAssayRunBusinessFields(current.Code, input.Name, input.Facility, input.Owner); err != nil {
		return model.AssayRun{}, err
	}
	current.Name = strings.TrimSpace(input.Name)
	current.Description = strings.TrimSpace(input.Description)
	current.Facility = strings.TrimSpace(input.Facility)
	current.Owner = strings.TrimSpace(input.Owner)
	current.Category = strings.TrimSpace(input.Category)
	current.RiskLevel = input.RiskLevel
	current.MetricValue = input.MetricValue
	current.MetricUnit = strings.TrimSpace(input.MetricUnit)
	current.EffectiveAt = input.EffectiveAt.UTC()
	current.Evidence = strings.TrimSpace(input.Evidence)
	current.RelatedCode = strings.ToUpper(strings.TrimSpace(input.RelatedCode))
	current.Version = input.ExpectedVersion + 1
	current.UpdatedAt = time.Now().UTC()

	// 已核验运行若经编辑成为严重风险，仍须补登危急处置事项，避免绕过闸门。
	var sideEffect func(tx *gorm.DB) error
	if current.Status == "validated" && current.RiskLevel == model.CriticalRiskLevel {
		sideEffect = func(tx *gorm.DB) error {
			_, ensureErr := s.disposition.EnsurePendingForRun(tx, &current, actor, requestID)
			return ensureErr
		}
	}
	if sideEffect != nil {
		if err := s.repository.UpdateTx(ctx, id, input.ExpectedVersion, &current, actor, requestID, sideEffect); err != nil {
			return model.AssayRun{}, fmt.Errorf("update 检测运行: %w", err)
		}
		return s.repository.Get(ctx, id)
	}
	if err := s.repository.Update(ctx, id, input.ExpectedVersion, &current); err != nil {
		return model.AssayRun{}, fmt.Errorf("update 检测运行: %w", err)
	}
	_ = s.security.Audit(ctx, actor, requestID, "update", "AssayRun", id, current.Status, current.Status, "updated business fields")
	return s.repository.Get(ctx, id)
}

func (s *assayRunService) Transition(ctx context.Context, id uint, input dto.TransitionRequest, actor, requestID string) (model.AssayRun, error) {
	current, err := s.repository.Get(ctx, id)
	if err != nil {
		return model.AssayRun{}, err
	}
	target := strings.TrimSpace(input.Status)
	if !constants.CanTransition(constants.AssayRunTransitions, current.Status, target) {
		return model.AssayRun{}, fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, current.Status, target)
	}
	before := current.Status
	current.Status = target
	current.Version = input.ExpectedVersion + 1
	current.OperatedBy = actor
	current.UpdatedAt = time.Now().UTC()

	// 危急检验结果处置闸门联动：核验通过后严重风险运行必须生成待处置事项；
	// 运行随后变为无效时，未失效事项全部作废并再次阻断关联结果。
	var sideEffect func(tx *gorm.DB) error
	if target == "validated" && current.RiskLevel == model.CriticalRiskLevel {
		sideEffect = func(tx *gorm.DB) error {
			_, ensureErr := s.disposition.EnsurePendingForRun(tx, &current, actor, requestID)
			return ensureErr
		}
	}
	if target == "invalid" {
		sideEffect = func(tx *gorm.DB) error {
			_, voidErr := s.disposition.VoidActiveForRun(tx, &current, actor, requestID,
				"assay run marked invalid; critical disposition voided and results blocked again")
			return voidErr
		}
	}

	if err := s.repository.TransitionTx(ctx, id, input.ExpectedVersion, &current, actor, requestID, before, target,
		strings.TrimSpace(input.Reason), sideEffect); err != nil {
		return model.AssayRun{}, fmt.Errorf("transition 检测运行: %w", err)
	}
	return s.repository.Get(ctx, id)
}

func (s *assayRunService) Delete(ctx context.Context, id uint, actor, requestID string) error {
	current, err := s.repository.Get(ctx, id)
	if err != nil {
		return err
	}
	if err := s.repository.Delete(ctx, id); err != nil {
		return err
	}
	return s.security.Audit(ctx, actor, requestID, "delete", "AssayRun", id, current.Status, "deleted", "soft deleted 检测运行")
}

func (s *assayRunService) StatusCounts(ctx context.Context) (map[string]int64, error) {
	return s.repository.CountByStatus(ctx)
}

func validateAssayRunBusinessFields(code, name, facility, owner string) error {
	if strings.TrimSpace(code) == "" || strings.TrimSpace(name) == "" || strings.TrimSpace(facility) == "" || strings.TrimSpace(owner) == "" {
		return ErrInvalidInput
	}
	return nil
}
