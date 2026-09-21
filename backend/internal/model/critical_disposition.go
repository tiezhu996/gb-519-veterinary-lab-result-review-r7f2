package model

import (
	"time"

	"gorm.io/gorm"
)

// CriticalDisposition 危急检验结果处置事项。检测运行核验通过且风险级别为
// critical 时自动生成；同一业务关联编号（relatedCode）下的结果签发在事项
// 确认前只能保留草稿。检测运行随后变为 invalid 时，原确认随事项作废。
type CriticalDisposition struct {
	ID                uint           `json:"id" gorm:"primaryKey"`
	Code              string         `json:"code" gorm:"size:64;uniqueIndex;not null"`
	Status            string         `json:"status" gorm:"size:40;index;not null"`
	Version           uint           `json:"version" gorm:"not null;default:1"`
	RelatedCode       string         `json:"relatedCode" gorm:"size:64;not null;index"`
	AssayRunID        uint           `json:"assayRunId" gorm:"not null;index"`
	AssayCode         string         `json:"assayCode" gorm:"size:64;not null;index"`
	RiskLevel         string         `json:"riskLevel" gorm:"size:32;not null"`
	RunOperator       string         `json:"runOperator" gorm:"size:80;not null;index"`
	ConfirmedBy       string         `json:"confirmedBy" gorm:"size:80;index"`
	ReceiveTarget     string         `json:"receiveTarget" gorm:"size:200"`
	DispositionAction string         `json:"dispositionAction" gorm:"size:1000"`
	ConfirmReason     string         `json:"confirmReason" gorm:"size:500"`
	ConfirmedAt       *time.Time     `json:"confirmedAt"`
	VoidedBy          string         `json:"voidedBy" gorm:"size:80;index"`
	VoidedReason      string         `json:"voidedReason" gorm:"size:500"`
	VoidedAt          *time.Time     `json:"voidedAt"`
	CreatedAt         time.Time      `json:"createdAt"`
	UpdatedAt         time.Time      `json:"updatedAt"`
	DeletedAt         gorm.DeletedAt `json:"-" gorm:"index"`
	// AssayStatus 是关联检测运行的当前状态，由仓储层 join 填充，不持久化。
	AssayStatus string `json:"assayStatus,omitempty" gorm:"-"`
}

func (item CriticalDisposition) TableName() string { return "critical_dispositions" }

var CriticalDispositionInitialStatus = "pending"
