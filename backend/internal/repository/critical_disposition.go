package repository

import (
	"context"
	"strings"
	"time"

	"github.com/blueship581/veterinary-lab-result-review/backend/internal/dto"
	"github.com/blueship581/veterinary-lab-result-review/backend/internal/model"
	"gorm.io/gorm"
)

// CriticalDispositionRepository owns all persistence operations for 危急检验结果处置闸门.
type CriticalDispositionRepository interface {
	List(context.Context, dto.PageQuery) (Page[model.CriticalDisposition], error)
	Get(context.Context, uint) (model.CriticalDisposition, error)

	// EnsurePendingForRun 在同一事务内为严重风险检测运行补登一条待处置事项；
	// 已存在未失效（pending/confirmed）事项时幂等返回既有事项。
	EnsurePendingForRun(tx *gorm.DB, run *model.AssayRun, actor, requestID string) (*model.CriticalDisposition, error)
	// VoidActiveForRun 将检测运行下全部未失效事项作废。
	VoidActiveForRun(tx *gorm.DB, run *model.AssayRun, actor, requestID, reason string) (int64, error)
	// Confirm 在事务内以条件更新完成确认，重复或并发确认只有一次成功。
	Confirm(ctx context.Context, id, expectedVersion uint, recipient, measure, actor, requestID string) (model.CriticalDisposition, error)

	// GatesForRelatedCodes 按业务关联编号计算当前生效的闸门快照。
	GatesForRelatedCodes(ctx context.Context, relatedCodes []string) (map[string]model.CriticalGate, error)
	CountByStatus(context.Context) (map[string]int64, error)
}

type criticalDispositionRepository struct {
	store *Store[model.CriticalDisposition]
	db    *gorm.DB
}

func NewCriticalDispositionRepository(db *gorm.DB) CriticalDispositionRepository {
	return &criticalDispositionRepository{store: NewStore[model.CriticalDisposition](db), db: db}
}

func (r *criticalDispositionRepository) List(ctx context.Context, q dto.PageQuery) (Page[model.CriticalDisposition], error) {
	page, pageSize := normalizePage(q.Page, q.PageSize)
	db := r.db.WithContext(ctx).Model(&model.CriticalDisposition{})
	if search := strings.TrimSpace(strings.ToLower(q.Search)); search != "" {
		wildcard := "%" + search + "%"
		db = db.Where("LOWER(code) LIKE ? OR LOWER(name) LIKE ? OR LOWER(related_code) LIKE ? OR LOWER(assay_run_code) LIKE ?", wildcard, wildcard, wildcard, wildcard)
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
	return Page[model.CriticalDisposition]{Items: items, Total: total, Page: page, PageSize: pageSize}, err
}

func (r *criticalDispositionRepository) Get(ctx context.Context, id uint) (model.CriticalDisposition, error) {
	var item model.CriticalDisposition
	err := r.db.WithContext(ctx).First(&item, id).Error
	return item, err
}

func (r *criticalDispositionRepository) CountByStatus(ctx context.Context) (map[string]int64, error) {
	return r.store.CountByStatus(ctx)
}

func (r *criticalDispositionRepository) EnsurePendingForRun(tx *gorm.DB, run *model.AssayRun, actor, requestID string) (*model.CriticalDisposition, error) {
	var active []model.CriticalDisposition
	if err := tx.Where("assay_run_id = ? AND status IN ?", run.ID, []string{model.DispositionStatePending, model.DispositionStateConfirmed}).
		Order("id DESC").Limit(1).Find(&active).Error; err != nil {
		return nil, err
	}
	if len(active) > 0 {
		return &active[0], nil
	}
	now := time.Now().UTC()
	item := &model.CriticalDisposition{
		BaseModel: model.BaseModel{
			Code:      dispositionCode(run),
			Name:      "危急结果处置-" + run.Code,
			Status:    model.DispositionStatePending,
			Version:   1,
			CreatedAt: now,
			UpdatedAt: now,
		},
		AssayRunID: run.ID, AssayRunCode: run.Code,
		RelatedCode: strings.ToUpper(strings.TrimSpace(run.RelatedCode)),
		RiskLevel:   run.RiskLevel,
		RunOperator: run.OperatedBy,
	}
	if strings.TrimSpace(item.RunOperator) == "" {
		item.RunOperator = "system"
	}
	if err := tx.Create(item).Error; err != nil {
		return nil, err
	}
	if err := appendAudit(tx, actor, requestID, "create", "CriticalDisposition", item.ID, "", item.Status,
		"critical assay run validated; pending disposition created for "+item.RelatedCode); err != nil {
		return nil, err
	}
	return item, nil
}

func (r *criticalDispositionRepository) VoidActiveForRun(tx *gorm.DB, run *model.AssayRun, actor, requestID, reason string) (int64, error) {
	var active []model.CriticalDisposition
	if err := tx.Where("assay_run_id = ? AND status IN ?", run.ID, []string{model.DispositionStatePending, model.DispositionStateConfirmed}).
		Find(&active).Error; err != nil {
		return 0, err
	}
	for index := range active {
		before := active[index].Status
		active[index].Status = model.DispositionStateVoid
		active[index].VoidReason = strings.TrimSpace(reason)
		active[index].Version++
		if err := tx.Model(&model.CriticalDisposition{}).
			Where("id = ?", active[index].ID).
			Select("status", "void_reason", "version", "updated_at").
			Updates(map[string]any{
				"status":      active[index].Status,
				"void_reason": active[index].VoidReason,
				"version":     active[index].Version,
				"updated_at":  time.Now().UTC(),
			}).Error; err != nil {
			return 0, err
		}
		if err := appendAudit(tx, actor, requestID, "void", "CriticalDisposition", active[index].ID, before,
			model.DispositionStateVoid, reason); err != nil {
			return 0, err
		}
	}
	return int64(len(active)), nil
}

func (r *criticalDispositionRepository) Confirm(ctx context.Context, id, expectedVersion uint, recipient, measure, actor, requestID string) (model.CriticalDisposition, error) {
	var confirmed model.CriticalDisposition
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var item model.CriticalDisposition
		if err := tx.First(&item, id).Error; err != nil {
			return err
		}
		if item.Status != model.DispositionStatePending {
			return ErrVersionConflict
		}
		now := time.Now().UTC()
		result := tx.Model(&model.CriticalDisposition{}).
			Where("id = ? AND version = ? AND status = ?", id, expectedVersion, model.DispositionStatePending).
			Updates(map[string]any{
				"status":             model.DispositionStateConfirmed,
				"recipient":          strings.TrimSpace(recipient),
				"measure":            strings.TrimSpace(measure),
				"confirmed_by":       actor,
				"confirmed_at":       now,
				"confirm_request_id": requestID,
				"void_reason":        "",
				"version":            expectedVersion + 1,
				"updated_at":         now,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrVersionConflict
		}
		if err := appendAudit(tx, actor, requestID, "confirm", "CriticalDisposition", id,
			model.DispositionStatePending, model.DispositionStateConfirmed,
			"critical result disposition confirmed; recipient="+strings.TrimSpace(recipient)); err != nil {
			return err
		}
		return tx.First(&confirmed, id).Error
	})
	return confirmed, err
}

func (r *criticalDispositionRepository) GatesForRelatedCodes(ctx context.Context, relatedCodes []string) (map[string]model.CriticalGate, error) {
	gates := make(map[string]model.CriticalGate)
	codes := make([]string, 0, len(relatedCodes))
	seen := make(map[string]bool)
	for _, code := range relatedCodes {
		normalized := strings.ToUpper(strings.TrimSpace(code))
		if normalized == "" || seen[normalized] {
			continue
		}
		seen[normalized] = true
		codes = append(codes, normalized)
	}
	if len(codes) == 0 {
		return gates, nil
	}
	var items []model.CriticalDisposition
	if err := r.db.WithContext(ctx).Where("related_code IN ?", codes).Order("id ASC").Find(&items).Error; err != nil {
		return nil, err
	}
	// 闸门取每个关联编号最新的一条事项（ID 最大）；任意未确认待处置或运行失效
	// 后的作废事项都会继续阻断关联结果进入复核。
	latest := make(map[string]model.CriticalDisposition)
	for _, item := range items {
		if current, ok := latest[item.RelatedCode]; !ok || item.ID > current.ID {
			latest[item.RelatedCode] = item
		}
	}
	for code, item := range latest {
		if item.Status != model.DispositionStatePending && item.Status != model.DispositionStateVoid &&
			item.Status != model.DispositionStateConfirmed {
			continue
		}
		gates[code] = model.CriticalGate{
			ID:           item.ID,
			Version:      item.Version,
			RelatedCode:  item.RelatedCode,
			Status:       item.Status,
			AssayRunCode: item.AssayRunCode,
			RunOperator:  item.RunOperator,
			Recipient:    item.Recipient,
			Measure:      item.Measure,
			ConfirmedBy:  item.ConfirmedBy,
			ConfirmedAt:  item.ConfirmedAt,
		}
	}
	return gates, nil
}

// dispositionCode 为每次重新核验生成唯一编码，同一运行可能在多轮失效/重核后
// 产生多条历史事项。
func dispositionCode(run *model.AssayRun) string {
	return "CD-" + strings.ReplaceAll(run.Code, "AR-", "") + "-V" + itoa(run.Version)
}

func itoa(value uint) string {
	if value == 0 {
		return "0"
	}
	digits := make([]byte, 0, 8)
	for value > 0 {
		digits = append([]byte{byte('0' + value%10)}, digits...)
		value /= 10
	}
	return string(digits)
}
