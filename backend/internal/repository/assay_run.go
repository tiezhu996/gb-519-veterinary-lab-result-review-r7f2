package repository

import (
	"context"

	"github.com/blueship581/veterinary-lab-result-review/backend/internal/dto"
	"github.com/blueship581/veterinary-lab-result-review/backend/internal/model"
	"gorm.io/gorm"
)

// AssayRunRepository owns all persistence operations for 检测运行.
type AssayRunRepository interface {
	List(context.Context, dto.PageQuery) (Page[model.AssayRun], error)
	Get(context.Context, uint) (model.AssayRun, error)
	Create(context.Context, *model.AssayRun) error
	Update(context.Context, uint, uint, *model.AssayRun) error
	// UpdateTx 完成乐观锁更新，并在同一事务内执行 sideEffect（危急闸门联动），
	// 最后原子写入审计日志。
	UpdateTx(ctx context.Context, id, version uint, item *model.AssayRun, actor, requestID string,
		sideEffect func(tx *gorm.DB) error) error
	// TransitionTx 完成乐观锁迁移，并在同一事务内执行 sideEffect（危急闸门联动），
	// 最后原子写入审计日志。
	TransitionTx(ctx context.Context, id, version uint, item *model.AssayRun, actor, requestID, before, target, reason string,
		sideEffect func(tx *gorm.DB) error) error
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
	page, err := r.store.List(ctx, q)
	if err != nil || len(page.Items) == 0 {
		return page, err
	}
	ids := make([]uint, 0, len(page.Items))
	for _, item := range page.Items {
		ids = append(ids, item.ID)
	}
	var dispositions []model.CriticalDisposition
	if err := r.db.WithContext(ctx).Where("assay_run_id IN ?", ids).
		Order("id DESC").Find(&dispositions).Error; err != nil {
		return Page[model.AssayRun]{}, err
	}
	byRun := make(map[uint][]model.CriticalDisposition)
	for _, disposition := range dispositions {
		byRun[disposition.AssayRunID] = append(byRun[disposition.AssayRunID], disposition)
	}
	for index := range page.Items {
		page.Items[index].Dispositions = byRun[page.Items[index].ID]
	}
	return page, nil
}
func (r *assayRunRepository) Get(ctx context.Context, id uint) (model.AssayRun, error) {
	var item model.AssayRun
	err := r.db.WithContext(ctx).Preload("Dispositions", func(db *gorm.DB) *gorm.DB {
		return db.Order("id DESC")
	}).First(&item, id).Error
	return item, err
}
func (r *assayRunRepository) Create(ctx context.Context, item *model.AssayRun) error {
	return r.store.Create(ctx, item)
}
func (r *assayRunRepository) Update(ctx context.Context, id, version uint, item *model.AssayRun) error {
	return r.store.Update(ctx, id, version, item)
}

func (r *assayRunRepository) UpdateTx(ctx context.Context, id, version uint, item *model.AssayRun, actor, requestID string,
	sideEffect func(tx *gorm.DB) error) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		item.Dispositions = nil
		if err := optimisticUpdate(tx, id, version, item); err != nil {
			return err
		}
		if sideEffect != nil {
			if err := sideEffect(tx); err != nil {
				return err
			}
		}
		return appendAudit(tx, actor, requestID, "update", "AssayRun", id, item.Status, item.Status, "updated business fields")
	})
}

func (r *assayRunRepository) TransitionTx(ctx context.Context, id, version uint, item *model.AssayRun, actor, requestID, before, target, reason string,
	sideEffect func(tx *gorm.DB) error) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		item.Dispositions = nil
		if err := optimisticUpdate(tx, id, version, item); err != nil {
			return err
		}
		if sideEffect != nil {
			if err := sideEffect(tx); err != nil {
				return err
			}
		}
		return appendAudit(tx, actor, requestID, "transition", "AssayRun", id, before, target, reason)
	})
}

func (r *assayRunRepository) Delete(ctx context.Context, id uint) error {
	return r.store.Delete(ctx, id)
}
func (r *assayRunRepository) CountByStatus(ctx context.Context) (map[string]int64, error) {
	return r.store.CountByStatus(ctx)
}
