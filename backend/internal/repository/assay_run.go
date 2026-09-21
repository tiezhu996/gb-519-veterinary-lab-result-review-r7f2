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

// AssayRunRepository owns all persistence operations for 检测运行.
type AssayRunRepository interface {
	List(context.Context, dto.PageQuery) (Page[model.AssayRun], error)
	Get(context.Context, uint) (model.AssayRun, error)
	Create(context.Context, *model.AssayRun) error
	Update(context.Context, uint, uint, *model.AssayRun) error
	TransitionValidated(context.Context, uint, uint, *model.AssayRun, string, string, string) error
	TransitionInvalidate(context.Context, uint, uint, *model.AssayRun, string, string, string) error
	Delete(context.Context, uint) error
	CountByStatus(context.Context) (map[string]int64, error)
}

type assayRunRepository struct {
	store *Store[model.AssayRun]
	db    *gorm.DB
}

func NewAssayRunRepository(db *gorm.DB) AssayRunRepository {
	return &assayRunRepository{store: NewStore[model.AssayRun](db), db: db}
}

func (r *assayRunRepository) List(ctx context.Context, q dto.PageQuery) (Page[model.AssayRun], error) {
	page, pageSize := normalizePage(q.Page, q.PageSize)
	db := r.db.WithContext(ctx).Model(&model.AssayRun{})
	if search := strings.TrimSpace(strings.ToLower(q.Search)); search != "" {
		wildcard := "%" + search + "%"
		db = db.Where("LOWER(code) LIKE ? OR LOWER(name) LIKE ?", wildcard, wildcard)
	}
	if status := q.Status; status != "" {
		db = db.Where("status = ?", status)
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return Page[model.AssayRun]{}, err
	}
	items := make([]model.AssayRun, 0)
	if err := db.Order("updated_at DESC, id DESC").
		Offset((page - 1) * pageSize).Limit(pageSize).Find(&items).Error; err != nil {
		return Page[model.AssayRun]{}, err
	}
	if err := r.attachDispositions(ctx, items); err != nil {
		return Page[model.AssayRun]{}, err
	}
	return Page[model.AssayRun]{Items: items, Total: total, Page: page, PageSize: pageSize}, nil
}

func (r *assayRunRepository) Get(ctx context.Context, id uint) (model.AssayRun, error) {
	var item model.AssayRun
	if err := r.db.WithContext(ctx).First(&item, id).Error; err != nil {
		return item, err
	}
	dispositions, err := r.dispositionRepo().ListByAssayRunIDs(ctx, []uint{item.ID})
	if err != nil {
		return item, err
	}
	item.Dispositions = dispositions[item.ID]
	return item, nil
}

// attachDispositions fills each run's critical dispositions without N+1 queries.
func (r *assayRunRepository) attachDispositions(ctx context.Context, items []model.AssayRun) error {
	if len(items) == 0 {
		return nil
	}
	ids := make([]uint, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	dispositions, err := r.dispositionRepo().ListByAssayRunIDs(ctx, ids)
	if err != nil {
		return err
	}
	for index := range items {
		items[index].Dispositions = dispositions[items[index].ID]
	}
	return nil
}

func (r *assayRunRepository) dispositionRepo() CriticalDispositionRepository {
	return NewCriticalDispositionRepository(r.db)
}

func (r *assayRunRepository) Create(ctx context.Context, item *model.AssayRun) error {
	return r.store.Create(ctx, item)
}
func (r *assayRunRepository) Update(ctx context.Context, id, version uint, item *model.AssayRun) error {
	return r.store.Update(ctx, id, version, item)
}

// TransitionValidated persists the validated transition and, when the run is
// critical, opens the gate by creating a fresh pending disposition in the same
// transaction. A previously voided disposition (from an invalid/validated
// cycle) remains historical; only one active disposition exists per run.
func (r *assayRunRepository) TransitionValidated(ctx context.Context, id, expectedVersion uint, item *model.AssayRun, actor, requestID, reason string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		item.Dispositions = nil
		if err := optimisticUpdate(tx, id, expectedVersion, item); err != nil {
			return err
		}
		if err := appendAudit(tx, actor, requestID, "transition", "AssayRun", id, "before", item.Status, reason); err != nil {
			return err
		}
		if item.RiskLevel == "critical" {
			var active int64
			if err := tx.Model(&model.CriticalDisposition{}).
				Where("assay_run_id = ? AND status = ?", id, model.CriticalDispositionInitialStatus).
				Count(&active).Error; err != nil {
				return err
			}
			if active == 0 {
				now := time.Now().UTC()
				disposition := model.CriticalDisposition{
					Code: generateDispositionCode(item.Code, now), Status: model.CriticalDispositionInitialStatus,
					Version: 1, RelatedCode: item.RelatedCode, AssayRunID: item.ID, AssayCode: item.Code,
					RiskLevel: item.RiskLevel, RunOperator: actor, CreatedAt: now, UpdatedAt: now,
				}
				if err := tx.Create(&disposition).Error; err != nil {
					return err
				}
				if err := appendAudit(tx, actor, requestID, "create", "CriticalDisposition", disposition.ID, "", "pending",
					"critical assay run validated; disposition required"); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// TransitionInvalidate persists the invalid transition and voids every
// disposition of the run (including confirmed ones), re-blocking related
// results, atomically.
func (r *assayRunRepository) TransitionInvalidate(ctx context.Context, id, expectedVersion uint, item *model.AssayRun, actor, requestID, reason string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		item.Dispositions = nil
		if err := optimisticUpdate(tx, id, expectedVersion, item); err != nil {
			return err
		}
		if err := appendAudit(tx, actor, requestID, "transition", "AssayRun", id, "before", item.Status, reason); err != nil {
			return err
		}
		now := time.Now().UTC()
		var dispositions []model.CriticalDisposition
		if err := tx.Where("assay_run_id = ? AND status <> ?", id, "voided").
			Clauses(clause.Locking{Strength: "UPDATE"}).Find(&dispositions).Error; err != nil {
			return err
		}
		for _, disposition := range dispositions {
			before := disposition.Status
			if err := tx.Model(&model.CriticalDisposition{}).
				Where("id = ?", disposition.ID).
				Updates(map[string]any{
					"status": "voided", "voided_by": actor, "voided_reason": reason,
					"voided_at": now, "updated_at": now,
				}).Error; err != nil {
				return err
			}
			if err := appendAudit(tx, actor, requestID, "void", "CriticalDisposition", disposition.ID,
				before, "voided", "assay run became invalid; prior confirmation no longer applies"); err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *assayRunRepository) Delete(ctx context.Context, id uint) error {
	return r.store.Delete(ctx, id)
}
func (r *assayRunRepository) CountByStatus(ctx context.Context) (map[string]int64, error) {
	return r.store.CountByStatus(ctx)
}

func generateDispositionCode(assayCode string, now time.Time) string {
	return "CD-" + assayCode + "-" + now.Format("20060102T150405.000000000")
}
