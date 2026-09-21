import { useState } from 'react';
import type { CriticalDisposition } from '../../types/domain';
import { confirmDisposition } from '../../api/critical-disposition';
import { useAuth } from '../../hooks/useAuth';
import { EmptyState } from './EmptyState';
import { UiButton } from './UiButton';
import { dispositionStatusLabel, isGateBlocked } from '../../utils/gate';

interface DispositionPanelProps {
  dispositions: CriticalDisposition[];
  /** reload parent pages after a successful confirmation */
  onChanged?: () => void;
}

const STATUS_TONE: Record<string, string> = {
  pending: 'status status--warning',
  confirmed: 'status status--success',
  voided: 'status status--danger',
};

export function DispositionPanel({ dispositions, onChanged }: DispositionPanelProps) {
  const { session } = useAuth();
  const [active, setActive] = useState<CriticalDisposition | null>(null);
  const [receiveTarget, setReceiveTarget] = useState('');
  const [dispositionAction, setDispositionAction] = useState('');
  const [reason, setReason] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [formError, setFormError] = useState('');

  if (!dispositions.length) return <EmptyState message="暂无危急处置事项" />;

  const openConfirm = (item: CriticalDisposition) => {
    setActive(item);
    setReceiveTarget('');
    setDispositionAction('');
    setReason('');
    setFormError('');
  };

  const closeConfirm = () => {
    setActive(null);
    setFormError('');
  };

  const submit = async () => {
    if (!active) return;
    if (!receiveTarget.trim() || !dispositionAction.trim() || !reason.trim()) {
      setFormError('接收对象、处置措施和确认原因均为必填项。');
      return;
    }
    setSubmitting(true);
    setFormError('');
    try {
      await confirmDisposition(active.id, {
        expectedVersion: active.version,
        receiveTarget: receiveTarget.trim(),
        dispositionAction: dispositionAction.trim(),
        reason: reason.trim(),
      });
      setActive(null);
      onChanged?.();
    } catch (error) {
      setFormError(error instanceof Error ? error.message : String(error));
    } finally {
      setSubmitting(false);
    }
  };

  const reviewer = session?.role === 'reviewer' || session?.role === 'admin';

  return <div className="disposition-strip">
    {dispositions.map((item) => {
      const blocking = isGateBlocked([item]);
      const canConfirm = reviewer && item.status === 'pending' && item.assayStatus === 'validated' &&
        session?.username !== item.runOperator;
      const blockedHint = !reviewer && item.status === 'pending' ? '仅复核员/管理员可确认'
        : session?.username === item.runOperator && item.status === 'pending' ? '确认人不能是检测运行操作员'
        : item.status === 'voided' ? '关联运行已无效，原确认失效'
        : '';
      return <article key={item.id} className={blocking ? 'disposition-card disposition-card--blocking' : 'disposition-card'}>
        <div className="result-title">
          <strong>{item.code}</strong>
          <span className={STATUS_TONE[item.status] || 'status status--neutral'}>{dispositionStatusLabel(item.status)}</span>
        </div>
        <small>关联运行：{item.assayCode}{item.assayStatus ? <em>（{item.assayStatus}）</em> : null} · 业务编号：{item.relatedCode}</small>
        <small>运行操作员：{item.runOperator || '-'}</small>
        {item.status === 'confirmed' && <>
          <small>确认人：{item.confirmedBy}</small>
          <small>接收对象：{item.receiveTarget}</small>
          <small>处置措施：{item.dispositionAction}</small>
        </>}
        {item.status === 'voided' && <small>失效原因：{item.voidedReason || '关联检测运行变为无效'}</small>}
        {blocking && <span className="gate-flag">处置闸门已阻断复核</span>}
        {item.status === 'pending' && item.assayStatus === 'validated' &&
          (canConfirm
            ? <UiButton onClick={() => openConfirm(item)}>确认处置事项</UiButton>
            : <span className="muted">{blockedHint || '等待处置'}</span>)}
      </article>;
    })}

    {active && <div className="modal-backdrop">
      <section className="modal" role="dialog" aria-modal="true">
        <h2>确认危急处置事项</h2>
        <p>{active.code} · 关联运行 {active.assayCode} · 运行操作员 {active.runOperator}</p>
        <div className="disposition-form">
          <label>接收对象
            <input aria-label="接收对象" value={receiveTarget} onChange={(e) => setReceiveTarget(e.target.value)} placeholder="例如：驻场首席兽医 / 属地兽医主管部门" />
          </label>
          <label>处置措施
            <textarea aria-label="处置措施" rows={3} value={dispositionAction} onChange={(e) => setDispositionAction(e.target.value)} placeholder="例如：立即隔离、启动复检、上报疫情" />
          </label>
          <label>确认原因
            <textarea aria-label="确认原因" rows={2} value={reason} onChange={(e) => setReason(e.target.value)} placeholder="确认依据与责任说明" />
          </label>
        </div>
        {formError && <div className="alert" role="alert">{formError}</div>}
        <footer>
          <button className="link-button" onClick={closeConfirm} disabled={submitting}>取消</button>
          <UiButton onClick={() => void submit()}>{submitting ? '提交中…' : '确认'}</UiButton>
        </footer>
      </section>
    </div>}
  </div>;
}
