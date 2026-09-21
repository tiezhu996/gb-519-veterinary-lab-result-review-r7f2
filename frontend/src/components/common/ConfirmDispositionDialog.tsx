import { useState } from 'react';
import type { DispositionItem } from '../../types/domain';
import { UiButton } from './UiButton';

interface ConfirmDispositionDialogProps {
  open: boolean;
  disposition: DispositionItem | null;
  submitting?: boolean;
  onCancel: () => void;
  onConfirm: (recipient: string, measure: string) => Promise<void> | void;
}

// ConfirmDispositionDialog 采集危急处置的接收对象与处置措施。确认人取自登录会话，
// 由后端强制为复核员/管理员且不得为检测运行操作员。
export function ConfirmDispositionDialog({ open, disposition, submitting = false, onCancel, onConfirm }: ConfirmDispositionDialogProps) {
  const [recipient, setRecipient] = useState('');
  const [measure, setMeasure] = useState('');
  if (!open || !disposition) return null;

  const submit = async () => {
    const nextRecipient = recipient.trim();
    const nextMeasure = measure.trim();
    if (nextRecipient.length < 2 || nextMeasure.length < 3) return;
    await onConfirm(nextRecipient, nextMeasure);
    setRecipient('');
    setMeasure('');
  };

  const close = () => {
    setRecipient('');
    setMeasure('');
    onCancel();
  };

  const valid = recipient.trim().length >= 2 && measure.trim().length >= 3;

  return <div className="modal-backdrop"><section className="modal modal--wide" role="dialog" aria-modal="true">
    <h2>确认危急检验结果处置</h2>
    <div className="disposition-form">
      <p className="disposition-hint">事项 <strong>{disposition.code}</strong>（关联运行 {disposition.assayRunCode} · 业务编号 {disposition.relatedCode}）确认前，关联结果只能保留草稿。</p>
      <label>接收对象
        <input aria-label="接收对象" placeholder="例如：值班首席兽医 / 临床主治团队" value={recipient} maxLength={160}
          onChange={(event) => setRecipient(event.target.value)} />
      </label>
      <label>处置措施
        <textarea aria-label="处置措施" placeholder="例如：立即隔离复检、通知临床并启动疫情上报" value={measure} maxLength={1000} rows={4}
          onChange={(event) => setMeasure(event.target.value)} />
      </label>
      <small className="muted">确认人须为复核员或管理员，且不能是检测运行操作员（{disposition.runOperator}）。</small>
    </div>
    <footer><button className="link-button" onClick={close} disabled={submitting}>取消</button>
      <UiButton onClick={() => void submit()} disabled={submitting || !valid}>{submitting ? '提交中…' : '确认处置'}</UiButton></footer>
  </section></div>;
}
