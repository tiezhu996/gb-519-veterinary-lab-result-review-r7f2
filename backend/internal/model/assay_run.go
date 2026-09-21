package model

import "time"

// AssayRun models 检测运行 as an independently versioned aggregate. The fields
// cover ownership, operational context, evidence and measured risk so later
// changes naturally span persistence, service and UI layers.
type AssayRun struct {
	BaseModel
	Facility    string    `json:"facility" gorm:"size:120;index"`
	Owner       string    `json:"owner" gorm:"size:120;index"`
	Category    string    `json:"category" gorm:"size:80;index"`
	RiskLevel   string    `json:"riskLevel" gorm:"size:32;index"`
	MetricValue float64   `json:"metricValue"`
	MetricUnit  string    `json:"metricUnit" gorm:"size:24"`
	EffectiveAt time.Time `json:"effectiveAt"`
	Evidence    string    `json:"evidence" gorm:"size:2000"`
	RelatedCode string    `json:"relatedCode" gorm:"size:64;index"`
	// OperatedBy 记录最近一次执行检测运行状态操作的操作员。运行核验通过时，
	// 该值被快照到危急处置事项，确认人不得与之为同一人。
	OperatedBy string `json:"operatedBy" gorm:"size:80;index"`

	// Dispositions 仅用于接口回读，不参与写入。
	Dispositions []CriticalDisposition `json:"dispositions,omitempty" gorm:"foreignKey:AssayRunID"`
}

func (item *AssayRun) GetBase() *BaseModel { return &item.BaseModel }

func (item AssayRun) TableName() string { return "assay_runs" }

var AssayRunInitialStatus = "planned"
