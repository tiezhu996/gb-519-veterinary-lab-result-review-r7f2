import { useMemo, useState } from 'react';
import type { DomainRecord, DispositionItem } from '../../types/domain';
import { confirmDisposition } from '../../api/disposition';
import { formatDate } from '../../utils/format';
import { DispositionBadge } from './DispositionBadge';
import { ConfirmDispositionDialog } from './ConfirmDispositionDialog';
import { EmptyState } from './EmptyState';
import { useAuth } from '../../hooks/useAuth';

interface CriticalGatePanelProps {
  records: DomainRecord[];
  // assay 变体展示每个运行附带的完整事项；signoff 变体使用结果上的闸门快照。
  variant: 'assay' | 'signoff';
  onChanged: () => void;
}

// NormalizedGate 统一检测运行事项与结果闸门快照两种形态，便于同一组件渲染。
interface NormalizedGate {
  key: string;
  id: number;
  version: number;
  code: string;
  status: string;
  relatedCode: string;
  assayRunCode: string;
  runOperator: string;
  recipient: string;
  measure: string;
  confirmedBy: string;
  confirmedAt?: string | null;
  voidReason?: string;
}

export function CriticalGatePanel({ records, variant, onChanged }: CriticalGatePanelProps) {
  const { session, hasRole } = useAuth();
  const [pending, setPending] = useState<DispositionItem | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState('');

  const gates = useMemo<NormalizedGate[]>(() => {
    if (variant === 'assay') {
      const collected: NormalizedGate[] = [];
      for (const record of records) {
        for (const item of record.dispositions || []) {
          collected.push({
            key: `run-${item.id}`, id: item.id, version: item.version, code: item.code, status: item.status,
            relatedCode: item.relatedCode, assayRunCode: item.assayRunCode, runOperator: item.runOperator,
            recipient: item.recipient, measure: item.measure, confirmedBy: item.confirmedBy,
            confirmedAt: item.confirmedAt, voidReason: item.voidReason,
          });
        }
      }
      return dedupeNewest(collected);
    }
    const collected: NormalizedGate[] = [];
    for (const record of records) {
      const gate = record.gate;
      if (!gate) continue;
      collected.push({
        key: `signoff-${gate.relatedCode || record.id}`, id: gate.id, version: gate.version, code: record.code, status: gate.status,
        relatedCode: gate.relatedCode, assayRunCode: gate.assayRunCode, runOperator: gate.runOperator,
        recipient: gate.recipient, measure: gate.measure, confirmedBy: gate.confirmedBy,
      });
    }
    return dedupeNewest(collected);
  }, [records, variant]);

  if (!gates.length) return <div className="gate-panel"><EmptyState message="暂无危急检验结果处置事项" /></div>;

  const canConfirmGate = (gate: NormalizedGate) => hasRole('reviewer')
    && gate.status === 'pending' && session?.username !== gate.runOperator;

  const blockReason = (gate: NormalizedGate) => {
    if (gate.status !== 'pending') return '';
    if (!hasRole('reviewer')) return '等待复核员/管理员确认';
    if (session?.username === gate.runOperator) return '操作员不可确认（异人约束）';
    return '';
  };

  const confirm = async (recipient: string, measure: string) => {
    if (!pending) return;
    setSubmitting(true);
    setError('');
    try {
      await confirmDisposition(pending.id, pending.version, recipient, measure);
      setPending(null);
      onChanged();
    } catch (confirmError) {
      setError(confirmError instanceof Error ? confirmError.message : String(confirmError));
    } finally {
      setSubmitting(false);
    }
  };

  return <div className="gate-panel">
    {error && <div className="alert" role="alert">{error}</div>}
    <div className="gate-list">
      {gates.map((gate) => <article key={gate.key} className={`gate-card gate-card--${gate.status}`}>
        <header>
          <div className="gate-title"><strong>{variant === 'assay' ? gate.code : gate.assayRunCode}</strong><DispositionBadge status={gate.status} /></div>
          <small>业务编号 <code>{gate.relatedCode || '-'}</code> · 关联运行 {gate.assayRunCode || '-'}</small>
        </header>
        <dl>
          <div><dt>操作员</dt><dd>{gate.runOperator || '-'}</dd></div>
          <div><dt>接收对象</dt><dd>{gate.recipient || '待填写'}</dd></div>
          <div><dt>处置措施</dt><dd>{gate.measure || '待填写'}</dd></div>
          <div><dt>确认人</dt><dd>{gate.confirmedBy || '待确认'}</dd></div>
          {gate.confirmedAt && <div><dt>确认时间</dt><dd>{formatDate(gate.confirmedAt)}</dd></div>}
          {gate.voidReason && <div className="gate-void"><dt>失效原因</dt><dd>{gate.voidReason}</dd></div>}
        </dl>
        {gate.status === 'pending' && (canConfirmGate(gate)
          ? <button className="table-action" onClick={() => setPending(toDispositionItem(gate))}>确认处置</button>
          : <small className="muted">{blockReason(gate)}</small>)}
      </article>)}
    </div>
    <ConfirmDispositionDialog open={Boolean(pending)} disposition={pending} submitting={submitting}
      onCancel={() => setPending(null)} onConfirm={confirm} />
  </div>;
}

function dedupeNewest(gates: NormalizedGate[]): NormalizedGate[] {
  // 结果页变体只有闸门快照（id 为 0），同一业务编号取第一条即可；检测页变体带
  // 完整事项（id > 0），同一业务编号保留最新（id 最大）的一条历史。
  const newest = new Map<string, NormalizedGate>();
  for (const gate of gates) {
    const key = gate.relatedCode || gate.key;
    const existing = newest.get(key);
    if (!existing) {
      newest.set(key, gate);
      continue;
    }
    if (gate.id > 0 && gate.id > existing.id) newest.set(key, gate);
  }
  return Array.from(newest.values());
}

function toDispositionItem(gate: NormalizedGate): DispositionItem {
  return {
    id: gate.id, code: gate.id ? `CD-${gate.id}` : gate.code, name: gate.code, status: gate.status, version: gate.version,
    assayRunId: 0, assayRunCode: gate.assayRunCode, relatedCode: gate.relatedCode, riskLevel: 'critical',
    runOperator: gate.runOperator, recipient: gate.recipient, measure: gate.measure,
    confirmedBy: gate.confirmedBy, confirmedAt: gate.confirmedAt, voidReason: gate.voidReason,
    createdAt: '', updatedAt: '',
  };
}
