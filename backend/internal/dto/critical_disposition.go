package dto

// ConfirmCriticalDisposition 是危急处置事项确认的写入契约。确认人身份来自会话，
// 不由客户端指定。
type ConfirmCriticalDisposition struct {
	ExpectedVersion   uint   `json:"expectedVersion" binding:"required"`
	ReceiveTarget     string `json:"receiveTarget" binding:"required,min=2,max=200"`
	DispositionAction string `json:"dispositionAction" binding:"required,min=4,max=1000"`
	Reason            string `json:"reason" binding:"required,min=3,max=500"`
}
