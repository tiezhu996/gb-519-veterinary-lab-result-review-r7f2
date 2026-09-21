package service

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/blueship581/veterinary-lab-result-review/backend/internal/config"
	"github.com/blueship581/veterinary-lab-result-review/backend/internal/dto"
	"github.com/blueship581/veterinary-lab-result-review/backend/internal/model"
	"github.com/blueship581/veterinary-lab-result-review/backend/internal/repository"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// gateAuditSecurity is a no-op SecurityService for gate tests: all audit
// entries are persisted transactionally inside repositories.
type gateAuditSecurity struct{}

func (gateAuditSecurity) Login(context.Context, dto.LoginRequest) (dto.LoginResponse, error) {
	return dto.LoginResponse{}, nil
}
func (gateAuditSecurity) Audit(context.Context, string, string, string, string, uint, string, string, string) error {
	return nil
}
func (gateAuditSecurity) ListAudits(context.Context, int, int, string) ([]model.AuditLog, int64, error) {
	return nil, 0, nil
}
func (gateAuditSecurity) AuditSummary(context.Context, time.Duration) (model.AuditSummary, error) {
	return model.AuditSummary{}, nil
}
func (gateAuditSecurity) EntityHistory(context.Context, string, uint, int) ([]model.AuditLog, error) {
	return nil, nil
}
func (gateAuditSecurity) RuntimeConfig() config.PublicConfig { return config.PublicConfig{} }

func TestCriticalDispositionGate(t *testing.T) {
	db := newGateTestDB(t)
	assayRepo := repository.NewAssayRunRepository(db)
	dispositionRepo := repository.NewCriticalDispositionRepository(db)
	signoffRepo := repository.NewResultSignoffRepository(db)
	audit := gateAuditSecurity{}
	assaySvc := NewAssayRunService(assayRepo, audit)
	dispositionSvc := NewCriticalDispositionService(dispositionRepo, assayRepo)
	signoffSvc := NewResultSignoffService(signoffRepo, dispositionRepo, audit)
	ctx := context.Background()

	const relatedCode = "REL-GATE-01"

	// 1. operator 创建并核验 critical 检测运行；核验通过必须生成待处置事项。
	assay := createGateAssay(t, ctx, assaySvc, "AR-GATE-01", "critical", relatedCode, "operator")
	assay = transitionGateAssay(t, ctx, assaySvc, assay, "running", "operator")
	assay = transitionGateAssay(t, ctx, assaySvc, assay, "validated", "operator")
	if assay.OperatedBy != "operator" {
		t.Fatalf("validated run must record operator, got %q", assay.OperatedBy)
	}
	if len(assay.Dispositions) != 1 || assay.Dispositions[0].Status != "pending" {
		t.Fatalf("validated critical run must open one pending disposition, got %#v", assay.Dispositions)
	}
	disposition := assay.Dispositions[0]
	if disposition.RunOperator != "operator" || disposition.RelatedCode != relatedCode {
		t.Fatalf("disposition snapshot mismatch: %#v", disposition)
	}

	// 2. 非 critical 运行核验不生成事项。
	normal := createGateAssay(t, ctx, assaySvc, "AR-GATE-02", "high", "REL-GATE-02", "operator")
	normal = transitionGateAssay(t, ctx, assaySvc, normal, "running", "operator")
	normal = transitionGateAssay(t, ctx, assaySvc, normal, "validated", "operator")
	if len(normal.Dispositions) != 0 {
		t.Fatalf("non-critical validated run must not open a disposition, got %#v", normal.Dispositions)
	}

	// 3. 同业务编号的 critical 结果提交复核必须被闸门阻断，只能保留草稿。
	signoff := createGateSignoff(t, ctx, signoffSvc, "RS-GATE-01", "critical", relatedCode, "operator")
	_, err := signoffSvc.Transition(ctx, signoff.ID, dto.TransitionRequest{
		Status: "peer_review", ExpectedVersion: signoff.Version, Reason: "try submit before disposition",
	}, "operator", model.RoleOperator, "gate-submit-blocked")
	if !errors.Is(err, ErrCriticalGate) {
		t.Fatalf("peer review must be blocked pending disposition, got %v", err)
	}
	blocked, err := dispositionRepo.HasBlockingGate(ctx, relatedCode)
	if err != nil || !blocked {
		t.Fatalf("gate should be blocking, blocked=%v err=%v", blocked, err)
	}

	// 4. 权限与异人约束：operator 不能确认；运行操作员本人（即使持有复核角色）不能确认。
	confirmInput := func(version uint) dto.ConfirmCriticalDisposition {
		return dto.ConfirmCriticalDisposition{
			ExpectedVersion: version, ReceiveTarget: "驻场首席兽医",
			DispositionAction: "立即隔离阳性动物并启动复检", Reason: "critical result handling protocol",
		}
	}
	if _, err := dispositionSvc.Confirm(ctx, disposition.ID, confirmInput(disposition.Version), "operator", model.RoleOperator, "gate-confirm-operator-denied"); !errors.Is(err, ErrReviewRequired) {
		t.Fatalf("operator confirm must require reviewer role, got %v", err)
	}
	if _, err := dispositionSvc.Confirm(ctx, disposition.ID, confirmInput(disposition.Version), "operator", model.RoleReviewer, "gate-confirm-same-operator-denied"); !errors.Is(err, ErrRunOperator) {
		t.Fatalf("run operator must not confirm own run, got %v", err)
	}
	if _, err := dispositionSvc.Confirm(ctx, disposition.ID, dto.ConfirmCriticalDisposition{
		ExpectedVersion: disposition.Version, ReceiveTarget: "  ", DispositionAction: "隔离",
		Reason: "missing target",
	}, "reviewer", model.RoleReviewer, "gate-confirm-missing-target"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("missing receive target/action must fail validation, got %v", err)
	}

	// 5. 独立 reviewer 确认成功，闸门打开，结果可进入复核。
	confirmed, err := dispositionSvc.Confirm(ctx, disposition.ID, confirmInput(disposition.Version), "reviewer", model.RoleReviewer, "gate-confirm-ok")
	if err != nil {
		t.Fatalf("independent reviewer confirmation: %v", err)
	}
	if confirmed.Status != "confirmed" || confirmed.ConfirmedBy != "reviewer" ||
		confirmed.ReceiveTarget != "驻场首席兽医" || confirmed.DispositionAction != "立即隔离阳性动物并启动复检" {
		t.Fatalf("unexpected confirmed disposition: %#v", confirmed)
	}
	blocked, err = dispositionRepo.HasBlockingGate(ctx, relatedCode)
	if err != nil || blocked {
		t.Fatalf("gate should be open after confirmation, blocked=%v err=%v", blocked, err)
	}
	peerReview, err := signoffSvc.Transition(ctx, signoff.ID, dto.TransitionRequest{
		Status: "peer_review", ExpectedVersion: signoff.Version, Reason: "gate cleared, submit for review",
	}, "operator", model.RoleOperator, "gate-submit-ok")
	if err != nil {
		t.Fatalf("peer review after confirmation must succeed: %v", err)
	}
	if peerReview.Status != "peer_review" {
		t.Fatalf("expected peer_review, got %s", peerReview.Status)
	}

	// 6. 同一事项重复确认只成功一次。
	if _, err := dispositionSvc.Confirm(ctx, confirmed.ID, confirmInput(confirmed.Version), "admin", model.RoleAdmin, "gate-confirm-repeat"); !errors.Is(err, ErrDispositionState) {
		t.Fatalf("repeat confirm must be rejected, got %v", err)
	}

	// 7. 并发确认只能成功一次（两个请求都基于同一 expectedVersion）。
	assay2 := createGateAssay(t, ctx, assaySvc, "AR-GATE-03", "critical", "REL-GATE-03", "operator")
	assay2 = transitionGateAssay(t, ctx, assaySvc, assay2, "running", "operator")
	assay2 = transitionGateAssay(t, ctx, assaySvc, assay2, "validated", "operator")
	d2 := assay2.Dispositions[0]
	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make([]error, 2)
	actors := []string{"reviewer", "admin"}
	for i := range 2 {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			<-start
			_, errs[index] = dispositionSvc.Confirm(ctx, d2.ID, confirmInput(d2.Version), actors[index], model.RoleReviewer, "gate-confirm-concurrent")
		}(i)
	}
	close(start)
	wg.Wait()
	successes, conflicts := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, repository.ErrAlreadyConfirmed),
			errors.Is(err, repository.ErrVersionConflict),
			errors.Is(err, ErrDispositionState):
			conflicts++
		default:
			t.Fatalf("unexpected concurrent confirm error: %v", err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("concurrent confirm must succeed exactly once, successes=%d conflicts=%d", successes, conflicts)
	}

	// 8. 检测运行随后变为 invalid：原确认作废，闸门再次阻断同编号结果。
	invalidated, err := assaySvc.Transition(ctx, assay.ID, dto.TransitionRequest{
		Status: "invalid", ExpectedVersion: assay.Version, Reason: "control failure invalidates run",
	}, "operator", "gate-invalidate")
	if err != nil {
		t.Fatalf("invalidate assay: %v", err)
	}
	if len(invalidated.Dispositions) != 1 || invalidated.Dispositions[0].Status != "voided" || invalidated.Dispositions[0].VoidedBy != "operator" {
		t.Fatalf("invalidation must void prior confirmation: %#v", invalidated.Dispositions)
	}
	blocked, err = dispositionRepo.HasBlockingGate(ctx, relatedCode)
	if err != nil || !blocked {
		t.Fatalf("gate must block again after invalidation, blocked=%v err=%v", blocked, err)
	}
	second := createGateSignoff(t, ctx, signoffSvc, "RS-GATE-02", "critical", relatedCode, "operator")
	if _, err := signoffSvc.Transition(ctx, second.ID, dto.TransitionRequest{
		Status: "peer_review", ExpectedVersion: second.Version, Reason: "try submit after invalidation",
	}, "operator", model.RoleOperator, "gate-resubmit-blocked"); !errors.Is(err, ErrCriticalGate) {
		t.Fatalf("re-blocked gate must reject peer review, got %v", err)
	}

	// 9. 运行重新核验通过：生成新的 pending 事项，旧的 voided 记录保留可追溯。
	revalidated, err := assaySvc.Transition(ctx, assay.ID, dto.TransitionRequest{
		Status: "validated", ExpectedVersion: invalidated.Version, Reason: "re-validate after corrective action",
	}, "operator2", "gate-revalidate")
	if err != nil {
		t.Fatalf("re-validate assay: %v", err)
	}
	if len(revalidated.Dispositions) != 2 {
		t.Fatalf("expected voided + new pending dispositions, got %#v", revalidated.Dispositions)
	}
	var pending *model.CriticalDisposition
	for index := range revalidated.Dispositions {
		if revalidated.Dispositions[index].Status == "pending" {
			pending = &revalidated.Dispositions[index]
		}
	}
	if pending == nil || pending.RunOperator != "operator2" {
		t.Fatalf("re-validation must open a new pending disposition operated by operator2: %#v", revalidated.Dispositions)
	}

	// 10. 普通（非 critical 编号）结果的异人复核规则保持不变。
	normalSignoff := createGateSignoff(t, ctx, signoffSvc, "RS-GATE-03", "medium", "REL-GATE-02", "operator")
	submitted, err := signoffSvc.Transition(ctx, normalSignoff.ID, dto.TransitionRequest{
		Status: "peer_review", ExpectedVersion: normalSignoff.Version, Reason: "normal result submit",
	}, "operator", model.RoleOperator, "normal-submit")
	if err != nil {
		t.Fatalf("normal result peer review must remain allowed: %v", err)
	}
	if _, err := signoffSvc.Transition(ctx, submitted.ID, dto.TransitionRequest{
		Status: "signed", ExpectedVersion: submitted.Version, Reason: "independent review",
	}, "operator", model.RoleOperator, "normal-self-sign-denied"); !errors.Is(err, ErrReviewRequired) {
		t.Fatalf("operator sign still forbidden for normal results, got %v", err)
	}
}

func newGateTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	// File-backed WAL database gives independent connections realistic
	// concurrency semantics (in-memory shared-cache SQLite deadlocks on
	// concurrent writers), matching the production MySQL row-lock behaviour.
	dsn := filepath.Join(t.TempDir(), "gate.db") + "?_journal_mode=WAL&_busy_timeout=5000"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.AuditLog{}, &model.AssayRun{}, &model.CriticalDisposition{},
		&model.ResultSignoff{}, &model.ResultSignoffRevision{}); err != nil {
		t.Fatalf("migrate sqlite: %v", err)
	}
	return db
}

func gateAssayInput(code, risk, related string) dto.CreateAssayRun {
	return dto.CreateAssayRun{
		Code: code, Name: "Critical gate assay", Facility: "Gate Lab", Owner: "Run desk",
		Category: "PCR", RiskLevel: risk, MetricValue: 42, MetricUnit: "score",
		EffectiveAt: time.Now().UTC(), Evidence: "controls within range", RelatedCode: related,
	}
}

func createGateAssay(t *testing.T, ctx context.Context, svc AssayRunService, code, risk, related, actor string) model.AssayRun {
	t.Helper()
	item, err := svc.Create(ctx, gateAssayInput(code, risk, related), actor, "gate-assay-create")
	if err != nil {
		t.Fatalf("create assay: %v", err)
	}
	return item
}

func transitionGateAssay(t *testing.T, ctx context.Context, svc AssayRunService, item model.AssayRun, target, actor string) model.AssayRun {
	t.Helper()
	updated, err := svc.Transition(ctx, item.ID, dto.TransitionRequest{
		Status: target, ExpectedVersion: item.Version, Reason: "gate assay transition " + target,
	}, actor, "gate-assay-"+target)
	if err != nil {
		t.Fatalf("transition assay to %s: %v", target, err)
	}
	return updated
}

func createGateSignoff(t *testing.T, ctx context.Context, svc ResultSignoffService, code, risk, related, actor string) model.ResultSignoff {
	t.Helper()
	input := signoffInput(code)
	input.RiskLevel = risk
	input.RelatedCode = related
	item, err := svc.Create(ctx, input, actor, "gate-signoff-create")
	if err != nil {
		t.Fatalf("create signoff: %v", err)
	}
	return item
}
