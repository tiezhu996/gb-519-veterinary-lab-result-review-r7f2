package dto

// ConfirmCriticalDisposition 是危急检验结果处置事项的确认契约。确认人由会话
// 提供（必须是 reviewer/admin 且不是检测运行操作员），接收对象和处置措施必填。
type ConfirmCriticalDisposition struct {
	ExpectedVersion uint   `json:"expectedVersion" binding:"required"`
	Recipient       string `json:"recipient" binding:"required,min=2,max=160"`
	Measure         string `json:"measure" binding:"required,min=3,max=1000"`
}
