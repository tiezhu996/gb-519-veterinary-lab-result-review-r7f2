
import type { DomainRecord } from '../../types/domain';
import { EmptyState } from './EmptyState';
import { StatusBadge } from './StatusBadge';
import { dispositionStatusLabel, isGateBlocked } from '../../utils/gate';

export function ResultPanel({ records }: { records: DomainRecord[] }) {
  if (!records.length) return <EmptyState message="暂无可展示的结果证据" />;
  return <div className="evidence-strip">{records.slice(0, 4).map((item) => {
    const latest = item.revisions?.at(-1);
    const activeDisposition = item.dispositions?.find((d) => d.status === 'pending' && d.assayStatus === 'validated')
      ?? item.dispositions?.find((d) => d.status === 'confirmed')
      ?? item.dispositions?.at(-1);
    const blocked = isGateBlocked(item.dispositions);
    return <article key={item.id}>
      <div className="result-title"><strong>{item.code}</strong><StatusBadge status={item.status} /></div>
      <span>{item.name}</span>
      <small title={item.evidence}>{item.evidence || '尚未附加证据'}</small>
      <small>v{item.version} · {latest?.actor || item.reviewedBy || item.preparedBy || item.operatedBy || item.owner}</small>
      <code title={latest?.requestId}>{latest?.requestId || '待形成签发请求 ID'}</code>
      {item.operatedBy && <small>运行操作员：{item.operatedBy}</small>}
      {activeDisposition && <div className="disposition-inline">
        <span className={`disposition-dot disposition-dot--${activeDisposition.status}`} />
        危急处置：{dispositionStatusLabel(activeDisposition.status)}
        {activeDisposition.confirmedBy ? ` · 确认人 ${activeDisposition.confirmedBy}` : ''}
        {activeDisposition.receiveTarget ? ` · 接收对象 ${activeDisposition.receiveTarget}` : ''}
        {activeDisposition.dispositionAction ? ` · 措施 ${activeDisposition.dispositionAction}` : ''}
        <small>关联运行：{activeDisposition.assayCode}</small>
      </div>}
      {blocked && <span className="gate-flag">处置闸门阻断中，仅可保留草稿</span>}
    </article>;
  })}</div>;
}
