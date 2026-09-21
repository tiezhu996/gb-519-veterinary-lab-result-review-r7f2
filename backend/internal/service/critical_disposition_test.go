package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/blueship581/veterinary-lab-result-review/backend/internal/dto"
	"github.com/blueship581/veterinary-lab-result-review/backend/internal/model"
	"github.com/blueship581/veterinary-lab-result-review/backend/internal/repository"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

type criticalGateHarness struct {
	db          *gorm.DB
	assay       AssayRunService
	signoff     ResultSignoffService
	disposition CriticalDispositionService
	dispRepo    repository.CriticalDispositionRepository
}

func newCriticalGateHarness(t *testing.T) criticalGateHarness {
	t.Helper()
	dsn := "file:" + t.Name() + "?mode=memory&cache=shared&_txlock=immediate"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.AuditLog{}, &model.AssayRun{}, &model.ResultSignoff{},
		&model.ResultSignoffRevision{}, &model.CriticalDisposition{}); err != nil {
		t.Fatalf("migrate sqlite: %v", err)
	}
	dispRepo := repository.NewCriticalDispositionRepository(db)
	assayRepo := repository.NewAssayRunRepository(db)
	signoffRepo := repository.NewResultSignoffRepository(db, dispRepo)
	return criticalGateHarness{
		db:          db,
		assay:       NewAssayRunService(assayRepo, dispRepo, nil),
		signoff:     NewResultSignoffService(signoffRepo, nil),
		disposition: NewCriticalDispositionService(dispRepo),
		dispRepo:    dispRepo,
	}
}

func seedAssayRun(t *testing.T, db *gorm.DB, code, riskLevel, status string) model.AssayRun {
	t.Helper()
	run := model.AssayRun{
		BaseModel: model.BaseModel{Code: code, Name: "critical assay " + code, Status: status, Version: 1},
		Facility:  "Veterinary Lab 3", Owner: "Field team", Category: "PCR", RiskLevel: riskLevel,
		MetricUnit: "score", EffectiveAt: time.Now().UTC(), Evidence: "control chart attached",
		RelatedCode: "REL-CRIT-1", OperatedBy: "operator",
	}
	if err := db.Create(&run).Error; err != nil {
		t.Fatalf("seed assay run: %v", err)
	}
	return run
}

func criticalSignoffInput(code string) dto.CreateResultSignoff {
	input := signoffInput(code)
	input.RiskLevel = "critical"
	input.RelatedCode = "REL-CRIT-1"
	return input
}

// TestCriticalGateFullFlow 覆盖：核验通过生成待处置 -> 草稿阻断复核 ->
// 异人确认（含接收对象/措施）-> 复核放行 -> 运行失效作废确认并再次阻断。
func TestCriticalGateFullFlow(t *testing.T) {
	h := newCriticalGateHarness(t)
	ctx := context.Background()
	run := seedAssayRun(t, h.db, "AR-GATE-1", model.CriticalRiskLevel, "running")

	// 普通（非严重）运行核验通过不产生处置事项。
	normal := seedAssayRun(t, h.db, "AR-GATE-N", "high", "running")
	normal.RelatedCode = "REL-NORMAL-1"
	if err := h.db.Model(&model.AssayRun{}).Where("id = ?", normal.ID).Update("related_code", "REL-NORMAL-1").Error; err != nil {
		t.Fatalf("adjust normal run: %v", err)
	}
	validatedNormal, err := h.assay.Transition(ctx, normal.ID, dto.TransitionRequest{
		Status: "validated", ExpectedVersion: 1, Reason: "normal run validated",
	}, "operator", "gate-normal-validate")
	if err != nil {
		t.Fatalf("validate normal run: %v", err)
	}
	if len(validatedNormal.Dispositions) != 0 {
		t.Fatalf("non-critical validated run must not create dispositions: %#v", validatedNormal.Dispositions)
	}

	// 严重运行核验通过后必须生成待处置事项。
	validated, err := h.assay.Transition(ctx, run.ID, dto.TransitionRequest{
		Status: "validated", ExpectedVersion: 1, Reason: "critical run validated",
	}, "operator", "gate-validate")
	if err != nil {
		t.Fatalf("validate critical run: %v", err)
	}
	if len(validated.Dispositions) != 1 || validated.Dispositions[0].Status != model.DispositionStatePending {
		t.Fatalf("validated critical run must expose one pending disposition: %#v", validated.Dispositions)
	}
	disposition := validated.Dispositions[0]
	if disposition.RunOperator != "operator" || disposition.RelatedCode != "REL-CRIT-1" {
		t.Fatalf("disposition snapshot mismatch: %#v", disposition)
	}

	// 同一业务关联编号的结果确认前只能保留草稿，不能进入复核。
	created, err := h.signoff.Create(ctx, criticalSignoffInput("RS-GATE-1"), "operator", "gate-signoff-create")
	if err != nil {
		t.Fatalf("create critical signoff: %v", err)
	}
	submit := dto.TransitionRequest{Status: "peer_review", ExpectedVersion: created.Version, Reason: "submit before disposition confirmed"}
	if _, err := h.signoff.Transition(ctx, created.ID, submit, "operator", model.RoleOperator, "gate-submit-blocked"); !errors.Is(err, ErrGateBlocked) {
		t.Fatalf("critical signoff must be blocked before disposition confirmation, got %v", err)
	}
	// 刷新后仍可回读到闸门快照。
	reread, err := h.signoff.Get(ctx, created.ID)
	if err != nil || reread.Gate == nil || reread.Gate.Status != model.DispositionStatePending {
		t.Fatalf("gate snapshot must survive refresh: %#v err=%v", reread.Gate, err)
	}

	// 检测运行操作员不能确认；缺接收对象/措施也不行。
	confirmInput := dto.ConfirmCriticalDisposition{
		ExpectedVersion: disposition.Version, Recipient: "驻场首席兽医", Measure: "立即隔离复检并上报",
	}
	if _, err := h.disposition.Confirm(ctx, disposition.ID, confirmInput, "operator", model.RoleReviewer, "gate-self-denied"); !errors.Is(err, ErrRunOperatorBlocked) {
		t.Fatalf("run operator confirming must be rejected, got %v", err)
	}
	missing := confirmInput
	missing.Recipient = ""
	if _, err := h.disposition.Confirm(ctx, disposition.ID, missing, "reviewer", model.RoleReviewer, "gate-missing-field"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("missing recipient must be rejected, got %v", err)
	}
	// operator 角色无权确认。
	if _, err := h.disposition.Confirm(ctx, disposition.ID, confirmInput, "operator2", model.RoleOperator, "gate-role-denied"); !errors.Is(err, ErrReviewRequired) {
		t.Fatalf("operator role must not confirm disposition, got %v", err)
	}

	// 复核员异人确认成功。
	confirmed, err := h.disposition.Confirm(ctx, disposition.ID, confirmInput, "reviewer", model.RoleReviewer, "gate-confirm")
	if err != nil {
		t.Fatalf("reviewer confirm: %v", err)
	}
	if confirmed.Status != model.DispositionStateConfirmed || confirmed.ConfirmedBy != "reviewer" ||
		confirmed.Recipient == "" || confirmed.Measure == "" || confirmed.ConfirmedAt == nil {
		t.Fatalf("confirmed disposition lost fields: %#v", confirmed)
	}

	// 同一事项重复确认只能成功一次。
	stale := confirmInput
	stale.ExpectedVersion = 1
	if _, err := h.disposition.Confirm(ctx, disposition.ID, stale, "admin", model.RoleAdmin, "gate-duplicate"); !errors.Is(err, ErrDispositionClosed) {
		t.Fatalf("duplicate confirmation must fail, got %v", err)
	}

	// 闸门放行后结果可进入复核；异人复核规则保持不变。
	peerReview, err := h.signoff.Transition(ctx, created.ID,
		dto.TransitionRequest{Status: "peer_review", ExpectedVersion: created.Version, Reason: "disposition confirmed, submit"},
		"operator", model.RoleOperator, "gate-submit-ok")
	if err != nil {
		t.Fatalf("submit after confirmation: %v", err)
	}
	if peerReview.Gate == nil || peerReview.Gate.Status != model.DispositionStateConfirmed {
		t.Fatalf("confirmed gate should still be readable: %#v", peerReview.Gate)
	}

	// 检测运行随后变为无效：原确认失效并再次阻断同一关联编号的新结果。
	invalidated, err := h.assay.Transition(ctx, run.ID, dto.TransitionRequest{
		Status: "invalid", ExpectedVersion: validated.Version, Reason: "control deviation discovered",
	}, "operator", "gate-invalidate")
	if err != nil {
		t.Fatalf("invalidate run: %v", err)
	}
	if len(invalidated.Dispositions) != 1 || invalidated.Dispositions[0].Status != model.DispositionStateVoid {
		t.Fatalf("disposition must be voided with its run: %#v", invalidated.Dispositions)
	}
	second, err := h.signoff.Create(ctx, criticalSignoffInput("RS-GATE-2"), "operator", "gate-signoff-second")
	if err != nil {
		t.Fatalf("create second critical signoff: %v", err)
	}
	if _, err := h.signoff.Transition(ctx, second.ID,
		dto.TransitionRequest{Status: "peer_review", ExpectedVersion: second.Version, Reason: "try after invalidation"},
		"operator", model.RoleOperator, "gate-reblock"); !errors.Is(err, ErrGateBlocked) {
		t.Fatalf("voided disposition must block associated results again, got %v", err)
	}

	// 重新核验会补登新的待处置事项，确认后再次放行。
	revalidated, err := h.assay.Transition(ctx, run.ID, dto.TransitionRequest{
		Status: "validated", ExpectedVersion: invalidated.Version, Reason: "controls re-run successfully",
	}, "operator", "gate-revalidate")
	if err != nil {
		t.Fatalf("revalidate run: %v", err)
	}
	if len(revalidated.Dispositions) != 2 {
		t.Fatalf("expected historical void plus new pending disposition, got %#v", revalidated.Dispositions)
	}
	var pending model.CriticalDisposition
	for _, item := range revalidated.Dispositions {
		if item.Status == model.DispositionStatePending {
			pending = item
		}
	}
	if pending.ID == 0 {
		t.Fatalf("revalidation must create a new pending disposition: %#v", revalidated.Dispositions)
	}
	if _, err := h.disposition.Confirm(ctx, pending.ID, dto.ConfirmCriticalDisposition{
		ExpectedVersion: pending.Version, Recipient: "驻场首席兽医", Measure: "复检通过，解除隔离观察",
	}, "reviewer", model.RoleReviewer, "gate-reconfirm"); err != nil {
		t.Fatalf("reconfirm new disposition: %v", err)
	}
	third, err := h.signoff.Create(ctx, criticalSignoffInput("RS-GATE-3"), "operator", "gate-signoff-third")
	if err != nil {
		t.Fatalf("create third critical signoff: %v", err)
	}
	if _, err := h.signoff.Transition(ctx, third.ID,
		dto.TransitionRequest{Status: "peer_review", ExpectedVersion: third.Version, Reason: "new disposition confirmed"},
		"operator", model.RoleOperator, "gate-resubmit-ok"); err != nil {
		t.Fatalf("newly confirmed disposition must release the gate, got %v", err)
	}
}

// TestCriticalGateAppearsOnEditToCritical 验证已核验运行经编辑成为严重风险时，
// 仍会补登待处置事项，不能通过编辑绕过闸门。
func TestCriticalGateAppearsOnEditToCritical(t *testing.T) {
	h := newCriticalGateHarness(t)
	ctx := context.Background()
	run := seedAssayRun(t, h.db, "AR-GATE-E", "high", "running")
	if _, err := h.assay.Transition(ctx, run.ID, dto.TransitionRequest{
		Status: "validated", ExpectedVersion: 1, Reason: "validate high run",
	}, "operator", "edit-validate"); err != nil {
		t.Fatalf("validate: %v", err)
	}
	update := dto.UpdateAssayRun{
		ExpectedVersion: 2, Name: "now critical assay", Facility: "Veterinary Lab 3", Owner: "Field team",
		Category: "PCR", RiskLevel: "critical", MetricUnit: "score",
		EffectiveAt: time.Now().UTC(), Evidence: "regraded to critical", RelatedCode: "REL-EDIT-1",
	}
	updated, err := h.assay.Update(ctx, run.ID, update, "operator", "edit-to-critical")
	if err != nil {
		t.Fatalf("edit run to critical: %v", err)
	}
	if len(updated.Dispositions) != 1 || updated.Dispositions[0].Status != model.DispositionStatePending {
		t.Fatalf("editing a validated run to critical must create a pending disposition: %#v", updated.Dispositions)
	}
}

// TestCriticalDispositionConcurrentConfirm 保证并发确认只有一次成功。
func TestCriticalDispositionConcurrentConfirm(t *testing.T) {
	h := newCriticalGateHarness(t)
	ctx := context.Background()
	run := seedAssayRun(t, h.db, "AR-GATE-C", model.CriticalRiskLevel, "running")
	validated, err := h.assay.Transition(ctx, run.ID, dto.TransitionRequest{
		Status: "validated", ExpectedVersion: 1, Reason: "validate",
	}, "operator", "concurrent-validate")
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	disposition := validated.Dispositions[0]

	const contenders = 8
	start := make(chan struct{})
	var wg sync.WaitGroup
	successes := make(chan int, contenders)
	for i := 0; i < contenders; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			<-start
			_, confirmErr := h.disposition.Confirm(ctx, disposition.ID, dto.ConfirmCriticalDisposition{
				ExpectedVersion: 1, Recipient: "驻场首席兽医", Measure: "并发确认处置措施",
			}, "reviewer", model.RoleReviewer, "concurrent-confirm")
			if confirmErr == nil {
				successes <- 1
			}
		}(i)
	}
	close(start)
	wg.Wait()
	close(successes)
	total := 0
	for range successes {
		total++
	}
	if total != 1 {
		t.Fatalf("exactly one concurrent confirmation must succeed, got %d", total)
	}
	final, err := h.disposition.Get(ctx, disposition.ID)
	if err != nil || final.Status != model.DispositionStateConfirmed || final.Version != 2 {
		t.Fatalf("final disposition mismatch: %#v err=%v", final, err)
	}
}
