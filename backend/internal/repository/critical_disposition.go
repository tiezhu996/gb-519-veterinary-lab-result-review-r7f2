package repository

import (
	"context"
	"strings"
	"time"

	"github.com/blueship581/veterinary-lab-result-review/backend/internal/dto"
	"github.com/blueship581/veterinary-lab-result-review/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// CriticalDispositionRepository owns persistence for 危急处置事项 and the
// join-based gate query shared by 结果签发.
type CriticalDispositionRepository interface {
	List(context.Context, dto.PageQuery) (Page[model.CriticalDisposition], error)
	Get(context.Context, uint) (model.CriticalDisposition, error)
	Confirm(context.Context, uint, uint, string, string, string, string, string) (model.CriticalDisposition, error)
	ListByAssayRunIDs(context.Context, []uint) (map[uint][]model.CriticalDisposition, error)
	ListByRelatedCodes(context.Context, []string) (map[string][]model.CriticalDisposition, error)
	HasBlockingGate(context.Context, string) (bool, error)
}

type criticalDispositionRepository struct {
	db *gorm.DB
}

func NewCriticalDispositionRepository(db *gorm.DB) CriticalDispositionRepository {
	return &criticalDispositionRepository{db: db}
}

func (r *criticalDispositionRepository) List(ctx context.Context, q dto.PageQuery) (Page[model.CriticalDisposition], error) {
	page, pageSize := normalizePage(q.Page, q.PageSize)
	db := r.db.WithContext(ctx).Model(&model.CriticalDisposition{})
	if search := strings.TrimSpace(strings.ToLower(q.Search)); search != "" {
		wildcard := "%" + search + "%"
		db = db.Where("LOWER(code) LIKE ? OR LOWER(related_code) LIKE ? OR LOWER(assay_code) LIKE ?", wildcard, wildcard, wildcard)
	}
	if status := strings.TrimSpace(q.Status); status != "" {
		db = db.Where("status = ?", status)
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return Page[model.CriticalDisposition]{}, err
	}
	items := make([]model.CriticalDisposition, 0)
	err := db.Order("updated_at DESC, id DESC").
		Offset((page - 1) * pageSize).Limit(pageSize).Find(&items).Error
	if err != nil {
		return Page[model.CriticalDisposition]{}, err
	}
	if err := r.fillAssayStatus(ctx, items); err != nil {
		return Page[model.CriticalDisposition]{}, err
	}
	return Page[model.CriticalDisposition]{Items: items, Total: total, Page: page, PageSize: pageSize}, nil
}

func (r *criticalDispositionRepository) Get(ctx context.Context, id uint) (model.CriticalDisposition, error) {
	var item model.CriticalDisposition
	err := r.db.WithContext(ctx).First(&item, id).Error
	if err != nil {
		return item, err
	}
	items := []model.CriticalDisposition{item}
	if err := r.fillAssayStatus(ctx, items); err != nil {
		return item, err
	}
	return items[0], nil
}

// fillAssayStatus joins the current assay run status onto each disposition so
// callers can evaluate the gate without an extra request.
func (r *criticalDispositionRepository) fillAssayStatus(ctx context.Context, items []model.CriticalDisposition) error {
	if len(items) == 0 {
		return nil
	}
	ids := make([]uint, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.AssayRunID)
	}
	var runs []model.AssayRun
	if err := r.db.WithContext(ctx).Select("id", "status").Where("id IN ?", ids).Find(&runs).Error; err != nil {
		return err
	}
	statusByID := make(map[uint]string, len(runs))
	for _, run := range runs {
		statusByID[run.ID] = run.Status
	}
	for index := range items {
		items[index].AssayStatus = statusByID[items[index].AssayRunID]
	}
	return nil
}

// Confirm atomically turns a pending disposition into confirmed. A row lock
// plus a conditional update guarantees that repeated or concurrent
// confirmations only succeed once; version mismatches surface as
// ErrVersionConflict and a non-pending row as ErrAlreadyConfirmed.
func (r *criticalDispositionRepository) Confirm(ctx context.Context, id, expectedVersion uint, confirmedBy, receiveTarget, action, reason, requestID string) (model.CriticalDisposition, error) {
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current model.CriticalDisposition
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, id).Error; err != nil {
			return err
		}
		if current.Status != model.CriticalDispositionInitialStatus {
			return ErrAlreadyConfirmed
		}
		now := time.Now().UTC()
		result := tx.Model(&model.CriticalDisposition{}).
			Where("id = ? AND version = ? AND status = ?", id, expectedVersion, model.CriticalDispositionInitialStatus).
			Updates(map[string]any{
				"status":             "confirmed",
				"version":            gorm.Expr("version + 1"),
				"confirmed_by":       confirmedBy,
				"receive_target":     receiveTarget,
				"disposition_action": action,
				"confirm_reason":     reason,
				"confirmed_at":       now,
				"updated_at":         now,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrVersionConflict
		}
		return appendAudit(tx, confirmedBy, requestID, "confirm", "CriticalDisposition", id, "pending", "confirmed", reason)
	})
	if err != nil {
		return model.CriticalDisposition{}, err
	}
	return r.Get(ctx, id)
}

func (r *criticalDispositionRepository) ListByAssayRunIDs(ctx context.Context, ids []uint) (map[uint][]model.CriticalDisposition, error) {
	result := make(map[uint][]model.CriticalDisposition)
	if len(ids) == 0 {
		return result, nil
	}
	var items []model.CriticalDisposition
	if err := r.db.WithContext(ctx).Where("assay_run_id IN ?", ids).
		Order("id").Find(&items).Error; err != nil {
		return nil, err
	}
	if err := r.fillAssayStatus(ctx, items); err != nil {
		return nil, err
	}
	for _, item := range items {
		result[item.AssayRunID] = append(result[item.AssayRunID], item)
	}
	return result, nil
}

func (r *criticalDispositionRepository) ListByRelatedCodes(ctx context.Context, codes []string) (map[string][]model.CriticalDisposition, error) {
	result := make(map[string][]model.CriticalDisposition)
	trimmed := make([]string, 0, len(codes))
	seen := make(map[string]bool)
	for _, code := range codes {
		upper := strings.ToUpper(strings.TrimSpace(code))
		if upper == "" || seen[upper] {
			continue
		}
		seen[upper] = true
		trimmed = append(trimmed, upper)
	}
	if len(trimmed) == 0 {
		return result, nil
	}
	var items []model.CriticalDisposition
	if err := r.db.WithContext(ctx).Where("related_code IN ?", trimmed).
		Order("id").Find(&items).Error; err != nil {
		return nil, err
	}
	if err := r.fillAssayStatus(ctx, items); err != nil {
		return nil, err
	}
	for _, item := range items {
		result[item.RelatedCode] = append(result[item.RelatedCode], item)
	}
	return result, nil
}

// HasBlockingGate reports whether related results are barred from peer review:
// a pending disposition on a validated run (awaiting confirmation), or a run
// that became invalid after a disposition was opened — the prior confirmation
// then no longer applies. Confirmation on a validated run keeps the gate open.
func (r *criticalDispositionRepository) HasBlockingGate(ctx context.Context, relatedCode string) (bool, error) {
	code := strings.ToUpper(strings.TrimSpace(relatedCode))
	if code == "" {
		return false, nil
	}
	var count int64
	err := r.db.WithContext(ctx).Model(&model.CriticalDisposition{}).
		Where("critical_dispositions.related_code = ? AND critical_dispositions.deleted_at IS NULL AND ("+
			"(critical_dispositions.status = ? AND assay_runs.status = ?) OR "+
			"(critical_dispositions.status = ? AND assay_runs.status = ?))",
			code,
			model.CriticalDispositionInitialStatus, "validated",
			"voided", "invalid").
		Joins("JOIN assay_runs ON assay_runs.id = critical_dispositions.assay_run_id AND assay_runs.deleted_at IS NULL").
		Count(&count).Error
	return count > 0, err
}
