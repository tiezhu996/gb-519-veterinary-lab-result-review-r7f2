package model

import "time"

// 危急检验结果处置闸门状态。pending 为待处置事项，confirmed 为已确认处置，
// void 表示关联检测运行随后变为无效，原确认（或待处置）随之失效并再次阻断。
const (
	DispositionStatePending   = "pending"
	DispositionStateConfirmed = "confirmed"
	DispositionStateVoid      = "void"
)

// CriticalRiskLevel 标识风险级别为“严重”的检验结果，只有该级别的记录在检测运行
// 核验通过后才会生成待处置事项。
const CriticalRiskLevel = "critical"

// CriticalDisposition models 危急检验结果处置闸门事项。事项以业务关联编号
// RelatedCode 关联同一批检测结果；同一检测运行在同一时间最多保留一条未失效事项。
type CriticalDisposition struct {
	BaseModel
	AssayRunID       uint       `json:"assayRunId" gorm:"not null;index"`
	AssayRunCode     string     `json:"assayRunCode" gorm:"size:64;index"`
	RelatedCode      string     `json:"relatedCode" gorm:"size:64;not null;index"`
	RiskLevel        string     `json:"riskLevel" gorm:"size:32;not null"`
	RunOperator      string     `json:"runOperator" gorm:"size:80;not null;index"`
	Recipient        string     `json:"recipient" gorm:"size:160"`
	Measure          string     `json:"measure" gorm:"size:1000"`
	ConfirmedBy      string     `json:"confirmedBy" gorm:"size:80;index"`
	ConfirmedAt      *time.Time `json:"confirmedAt,omitempty"`
	ConfirmRequestID string     `json:"-" gorm:"size:64"`
	VoidReason       string     `json:"voidReason" gorm:"size:500"`
}

func (item *CriticalDisposition) GetBase() *BaseModel { return &item.BaseModel }

func (item CriticalDisposition) TableName() string { return "critical_dispositions" }

var CriticalDispositionInitialStatus = DispositionStatePending

// CriticalGate 是检测与结果页面读取的处置闸门快照（不落库）。
type CriticalGate struct {
	ID           uint       `json:"id"`
	Version      uint       `json:"version"`
	RelatedCode  string     `json:"relatedCode"`
	Status       string     `json:"status"`
	AssayRunCode string     `json:"assayRunCode"`
	RunOperator  string     `json:"runOperator"`
	Recipient    string     `json:"recipient"`
	Measure      string     `json:"measure"`
	ConfirmedBy  string     `json:"confirmedBy"`
	ConfirmedAt  *time.Time `json:"confirmedAt,omitempty"`
}
