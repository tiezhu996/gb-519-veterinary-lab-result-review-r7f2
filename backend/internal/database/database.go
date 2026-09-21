package database

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/blueship581/veterinary-lab-result-review/backend/internal/config"
	"github.com/blueship581/veterinary-lab-result-review/backend/internal/model"
	"github.com/glebarez/sqlite"
	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func Open(ctx context.Context, cfg config.Config, log *slog.Logger) (*gorm.DB, *redis.Client, error) {
	var dialector gorm.Dialector
	switch cfg.DatabaseDriver {
	case "postgres":
		dialector = postgres.Open(cfg.DatabaseDSN)
	case "mysql":
		dialector = mysql.Open(cfg.DatabaseDSN)
	case "sqlite":
		dialector = sqlite.Open(cfg.DatabaseDSN)
	default:
		return nil, nil, fmt.Errorf("unsupported database driver %q", cfg.DatabaseDriver)
	}
	logLevel := logger.Warn
	if cfg.Environment == "development" {
		logLevel = logger.Info
	}
	var db *gorm.DB
	var err error
	for attempt := 1; attempt <= 20; attempt++ {
		db, err = gorm.Open(dialector, &gorm.Config{Logger: logger.Default.LogMode(logLevel)})
		if err == nil {
			sqlDB, dbErr := db.DB()
			if dbErr == nil && sqlDB.PingContext(ctx) == nil {
				break
			}
			if dbErr != nil {
				err = dbErr
			} else {
				err = sqlDB.PingContext(ctx)
			}
		}
		log.Warn("database not ready", "attempt", attempt, "error", err)
		select {
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		case <-time.After(time.Second):
		}
	}
	if err != nil {
		return nil, nil, fmt.Errorf("connect database: %w", err)
	}
	if err := migrate(db); err != nil {
		return nil, nil, err
	}
	if err := Seed(ctx, db); err != nil {
		return nil, nil, err
	}
	var redisClient *redis.Client
	if cfg.RedisAddr != "" {
		redisClient = redis.NewClient(&redis.Options{Addr: cfg.RedisAddr, Password: cfg.RedisPassword})
		if err := redisClient.Ping(ctx).Err(); err != nil {
			return nil, nil, fmt.Errorf("connect redis: %w", err)
		}
	}
	return db, redisClient, nil
}

func migrate(db *gorm.DB) error {
	return db.AutoMigrate(
		&model.User{}, &model.AuditLog{},
		&model.AnimalCase{},
		&model.Specimen{},
		&model.AssayRun{},
		&model.ResultSignoff{},
		&model.ResultSignoffRevision{},
		&model.CriticalDisposition{},
	)
}

func Seed(ctx context.Context, db *gorm.DB) error {
	var users int64
	if err := db.WithContext(ctx).Model(&model.User{}).Count(&users).Error; err != nil {
		return err
	}
	if users == 0 {
		password, err := bcrypt.GenerateFromPassword([]byte("Admin123!"), bcrypt.DefaultCost)
		if err != nil {
			return err
		}
		seedUsers := []model.User{
			{Username: "admin", DisplayName: "系统管理员", PasswordHash: string(password), Role: model.RoleAdmin, Active: true},
			{Username: "reviewer", DisplayName: "质量复核员", PasswordHash: string(password), Role: model.RoleReviewer, Active: true},
			{Username: "operator", DisplayName: "现场操作员", PasswordHash: string(password), Role: model.RoleOperator, Active: true},
			{Username: "viewer", DisplayName: "只读审计员", PasswordHash: string(password), Role: model.RoleViewer, Active: true},
		}
		if err := db.WithContext(ctx).Create(&seedUsers).Error; err != nil {
			return err
		}
	}

	if err := seedAnimalCase(ctx, db); err != nil {
		return err
	}

	if err := seedSpecimen(ctx, db); err != nil {
		return err
	}

	if err := seedAssayRun(ctx, db); err != nil {
		return err
	}

	if err := seedResultSignoff(ctx, db); err != nil {
		return err
	}

	if err := seedCriticalDisposition(ctx, db); err != nil {
		return err
	}

	return nil
}

func seedAnimalCase(ctx context.Context, db *gorm.DB) error {
	var count int64
	if err := db.WithContext(ctx).Model(&model.AnimalCase{}).Count(&count).Error; err != nil || count > 0 {
		return err
	}
	now := time.Now().UTC()
	items := []model.AnimalCase{

		{BaseModel: model.BaseModel{Code: "AC-001", Name: "动物样本来源示例一", Status: "registered", Version: 1,
			Description: "用于启动验证和主要流程演示的动物样本来源记录"}, Facility: "兽医检验样本结果复核区域1", Owner: "运行一组",
			Category: "常规", RiskLevel: "low", MetricValue: 12.5, MetricUnit: "unit",
			EffectiveAt: now.Add(0 * time.Hour), Evidence: "已完成基础证据核对", RelatedCode: "REL-519-01"},

		{BaseModel: model.BaseModel{Code: "AC-002", Name: "动物样本来源示例二", Status: "sampling", Version: 1,
			Description: "用于启动验证和主要流程演示的动物样本来源记录"}, Facility: "兽医检验样本结果复核区域2", Owner: "质量复核组",
			Category: "重点", RiskLevel: "medium", MetricValue: 25.0, MetricUnit: "%",
			EffectiveAt: now.Add(3 * time.Hour), Evidence: "已完成基础证据核对", RelatedCode: "REL-519-02"},

		{BaseModel: model.BaseModel{Code: "AC-003", Name: "动物样本来源示例三", Status: "testing", Version: 1,
			Description: "用于启动验证和主要流程演示的动物样本来源记录"}, Facility: "兽医检验样本结果复核区域3", Owner: "安全主管组",
			Category: "复核", RiskLevel: "high", MetricValue: 37.5, MetricUnit: "score",
			EffectiveAt: now.Add(6 * time.Hour), Evidence: "已完成基础证据核对", RelatedCode: "REL-519-03"},
	}
	return db.WithContext(ctx).Create(&items).Error
}

func seedSpecimen(ctx context.Context, db *gorm.DB) error {
	var count int64
	if err := db.WithContext(ctx).Model(&model.Specimen{}).Count(&count).Error; err != nil || count > 0 {
		return err
	}
	now := time.Now().UTC()
	items := []model.Specimen{

		{BaseModel: model.BaseModel{Code: "S-001", Name: "检验样本示例一", Status: "received", Version: 1,
			Description: "用于启动验证和主要流程演示的检验样本记录"}, Facility: "兽医检验样本结果复核区域1", Owner: "运行一组",
			Category: "常规", RiskLevel: "low", MetricValue: 12.5, MetricUnit: "unit",
			EffectiveAt: now.Add(0 * time.Hour), Evidence: "已完成基础证据核对", RelatedCode: "REL-519-01"},

		{BaseModel: model.BaseModel{Code: "S-002", Name: "检验样本示例二", Status: "testing", Version: 1,
			Description: "用于启动验证和主要流程演示的检验样本记录"}, Facility: "兽医检验样本结果复核区域2", Owner: "质量复核组",
			Category: "重点", RiskLevel: "medium", MetricValue: 25.0, MetricUnit: "%",
			EffectiveAt: now.Add(3 * time.Hour), Evidence: "已完成基础证据核对", RelatedCode: "REL-519-02"},

		{BaseModel: model.BaseModel{Code: "S-003", Name: "检验样本示例三", Status: "hold", Version: 1,
			Description: "用于启动验证和主要流程演示的检验样本记录"}, Facility: "兽医检验样本结果复核区域3", Owner: "安全主管组",
			Category: "复核", RiskLevel: "high", MetricValue: 37.5, MetricUnit: "score",
			EffectiveAt: now.Add(6 * time.Hour), Evidence: "已完成基础证据核对", RelatedCode: "REL-519-03"},
	}
	return db.WithContext(ctx).Create(&items).Error
}

func seedAssayRun(ctx context.Context, db *gorm.DB) error {
	var count int64
	if err := db.WithContext(ctx).Model(&model.AssayRun{}).Count(&count).Error; err != nil || count > 0 {
		return err
	}
	now := time.Now().UTC()
	items := []model.AssayRun{

		{BaseModel: model.BaseModel{Code: "AR-001", Name: "检测运行示例一", Status: "planned", Version: 1,
			Description: "用于启动验证和主要流程演示的检测运行记录"}, Facility: "兽医检验样本结果复核区域1", Owner: "运行一组",
			Category: "常规", RiskLevel: "low", MetricValue: 12.5, MetricUnit: "unit",
			EffectiveAt: now.Add(0 * time.Hour), Evidence: "已完成基础证据核对", RelatedCode: "REL-519-01", OperatedBy: "operator"},

		{BaseModel: model.BaseModel{Code: "AR-002", Name: "检测运行示例二", Status: "running", Version: 1,
			Description: "用于启动验证和主要流程演示的检测运行记录"}, Facility: "兽医检验样本结果复核区域2", Owner: "质量复核组",
			Category: "重点", RiskLevel: "medium", MetricValue: 25.0, MetricUnit: "%",
			EffectiveAt: now.Add(3 * time.Hour), Evidence: "已完成基础证据核对", RelatedCode: "REL-519-02", OperatedBy: "operator"},

		{BaseModel: model.BaseModel{Code: "AR-003", Name: "检测运行示例三（严重·已处置）", Status: "validated", Version: 1,
			Description: "严重风险检测运行，处置事项已由复核员确认"}, Facility: "兽医检验样本结果复核区域3", Owner: "安全主管组",
			Category: "复核", RiskLevel: "critical", MetricValue: 37.5, MetricUnit: "score",
			EffectiveAt: now.Add(6 * time.Hour), Evidence: "危急值阳性，已完成运行核验", RelatedCode: "REL-519-03", OperatedBy: "operator"},

		{BaseModel: model.BaseModel{Code: "AR-004", Name: "检测运行示例四（严重·待处置）", Status: "validated", Version: 1,
			Description: "严重风险检测运行，待处置事项确认前关联结果只能保留草稿"}, Facility: "兽医检验样本结果复核区域4", Owner: "应急处置组",
			Category: "复核", RiskLevel: "critical", MetricValue: 62.0, MetricUnit: "score",
			EffectiveAt: now.Add(9 * time.Hour), Evidence: "危急值阳性，已完成运行核验", RelatedCode: "REL-519-04", OperatedBy: "operator"},

		{BaseModel: model.BaseModel{Code: "AR-005", Name: "检测运行示例五（严重·运行失效）", Status: "invalid", Version: 2,
			Description: "严重风险检测运行核验后被判无效，原处置确认失效并再次阻断关联结果"}, Facility: "兽医检验样本结果复核区域5", Owner: "应急处置组",
			Category: "复核", RiskLevel: "critical", MetricValue: 58.5, MetricUnit: "score",
			EffectiveAt: now.Add(12 * time.Hour), Evidence: "复检发现对照异常，运行判无效", RelatedCode: "REL-519-05", OperatedBy: "operator"},
	}
	return db.WithContext(ctx).Create(&items).Error
}

func seedResultSignoff(ctx context.Context, db *gorm.DB) error {
	var count int64
	if err := db.WithContext(ctx).Model(&model.ResultSignoff{}).Count(&count).Error; err != nil || count > 0 {
		return err
	}
	now := time.Now().UTC()
	items := []model.ResultSignoff{

		{BaseModel: model.BaseModel{Code: "RS-001", Name: "结果签发示例一", Status: "draft", Version: 1,
			Description: "用于启动验证和主要流程演示的结果签发记录"}, Facility: "兽医检验样本结果复核区域1", Owner: "运行一组",
			Category: "常规", RiskLevel: "low", MetricValue: 12.5, MetricUnit: "unit",
			EffectiveAt: now.Add(0 * time.Hour), Evidence: "已完成基础证据核对", RelatedCode: "REL-519-01", PreparedBy: "operator"},

		{BaseModel: model.BaseModel{Code: "RS-002", Name: "结果签发示例二", Status: "peer_review", Version: 1,
			Description: "用于启动验证和主要流程演示的结果签发记录"}, Facility: "兽医检验样本结果复核区域2", Owner: "质量复核组",
			Category: "重点", RiskLevel: "medium", MetricValue: 25.0, MetricUnit: "%",
			EffectiveAt: now.Add(3 * time.Hour), Evidence: "已完成基础证据核对", RelatedCode: "REL-519-02", PreparedBy: "operator"},

		{BaseModel: model.BaseModel{Code: "RS-003", Name: "结果签发示例三", Status: "signed", Version: 1,
			Description: "严重风险结果，处置事项已确认后完成异人签发"}, Facility: "兽医检验样本结果复核区域3", Owner: "安全主管组",
			Category: "复核", RiskLevel: "critical", MetricValue: 37.5, MetricUnit: "score",
			EffectiveAt: now.Add(6 * time.Hour), Evidence: "已完成基础证据核对", RelatedCode: "REL-519-03", PreparedBy: "operator", ReviewedBy: "reviewer", ReviewReason: "演示数据双人复核通过"},

		{BaseModel: model.BaseModel{Code: "RS-004", Name: "结果签发示例四（严重·草稿）", Status: "draft", Version: 1,
			Description: "待处置事项确认前只能保留草稿，不能进入复核"}, Facility: "兽医检验样本结果复核区域4", Owner: "应急处置组",
			Category: "复核", RiskLevel: "critical", MetricValue: 62.0, MetricUnit: "score",
			EffectiveAt: now.Add(9 * time.Hour), Evidence: "危急值结果待处置", RelatedCode: "REL-519-04", PreparedBy: "operator"},

		{BaseModel: model.BaseModel{Code: "RS-005", Name: "结果签发示例五（严重·阻断）", Status: "draft", Version: 1,
			Description: "检测运行失效后原处置确认作废，关联结果再次被阻断"}, Facility: "兽医检验样本结果复核区域5", Owner: "应急处置组",
			Category: "复核", RiskLevel: "critical", MetricValue: 58.5, MetricUnit: "score",
			EffectiveAt: now.Add(12 * time.Hour), Evidence: "运行失效，处置链路待重建", RelatedCode: "REL-519-05", PreparedBy: "operator"},
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&items).Error; err != nil {
			return err
		}
		revisions := make([]model.ResultSignoffRevision, 0, len(items))
		for _, item := range items {
			revisions = append(revisions, model.ResultSignoffRevision{
				ResultSignoffID: item.ID, Version: item.Version, Status: item.Status,
				Evidence: item.Evidence, Actor: "system-seed", RequestID: "seed-gb-519",
				Action: "seed", Reason: "initial demonstration signoff", CreatedAt: now,
			})
		}
		return tx.Create(&revisions).Error
	})
}

func seedCriticalDisposition(ctx context.Context, db *gorm.DB) error {
	var count int64
	if err := db.WithContext(ctx).Model(&model.CriticalDisposition{}).Count(&count).Error; err != nil || count > 0 {
		return err
	}

	type runRef struct {
		code        string
		status      string
		relatedCode string
	}
	refs := []runRef{
		{code: "AR-003", status: model.DispositionStateConfirmed, relatedCode: "REL-519-03"},
		{code: "AR-004", status: model.DispositionStatePending, relatedCode: "REL-519-04"},
		{code: "AR-005", status: model.DispositionStateVoid, relatedCode: "REL-519-05"},
	}
	now := time.Now().UTC()
	items := make([]model.CriticalDisposition, 0, len(refs))
	for index, ref := range refs {
		var run model.AssayRun
		if err := db.WithContext(ctx).Where("code = ?", ref.code).First(&run).Error; err != nil {
			return err
		}
		item := model.CriticalDisposition{
			BaseModel: model.BaseModel{
				Code: "CD-" + strings.TrimPrefix(run.Code, "AR-") + "-V1",
				Name: "危急结果处置-" + run.Code, Status: ref.status, Version: 1,
			},
			AssayRunID: run.ID, AssayRunCode: run.Code, RelatedCode: ref.relatedCode,
			RiskLevel: model.CriticalRiskLevel, RunOperator: "operator",
		}
		switch ref.status {
		case model.DispositionStateConfirmed:
			confirmedAt := now.Add(time.Duration(index) * time.Hour)
			item.Recipient = "驻场首席兽医张医生"
			item.Measure = "立即隔离复检并启动疫情上报预案，2 小时内复核结果"
			item.ConfirmedBy = "reviewer"
			item.ConfirmedAt = &confirmedAt
			item.ConfirmRequestID = "seed-gb-519"
		case model.DispositionStateVoid:
			item.VoidReason = "检测运行判无效，原危急处置确认失效"
		}
		items = append(items, item)
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&items).Error; err != nil {
			return err
		}
		logs := make([]model.AuditLog, 0, len(items))
		for _, item := range items {
			action := "create"
			detail := "seed critical disposition"
			switch item.Status {
			case model.DispositionStateConfirmed:
				action = "confirm"
				detail = "seed confirmed critical disposition"
			case model.DispositionStateVoid:
				action = "void"
				detail = "seed voided critical disposition"
			}
			logs = append(logs, model.AuditLog{
				RequestID: "seed-gb-519", Actor: "system-seed", Action: action,
				EntityType: "CriticalDisposition", EntityID: item.ID,
				BeforeState: model.DispositionStatePending, AfterState: item.Status,
				Detail: detail, CreatedAt: now,
			})
		}
		return tx.Create(&logs).Error
	})
}
