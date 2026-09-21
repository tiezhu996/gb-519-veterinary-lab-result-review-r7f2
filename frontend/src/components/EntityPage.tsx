import { useEffect, useMemo, useState } from 'react';
import type { EntityConfig, DomainRecord, CriticalDisposition } from '../types/domain';
import type { EntityStore } from '../stores/factory';
import { nextStatus, formatDate } from '../utils/format';
import { isGateBlocked } from '../utils/gate';
import { StatusBadge } from './common/StatusBadge';
import { RiskTag } from './common/RiskTag';
import { ResultPanel } from './common/ResultPanel';
import { DispositionPanel } from './common/DispositionPanel';
import { EmptyState } from './common/EmptyState';
import { MetricCard } from './common/MetricCard';
import { ConfirmDialog } from './common/ConfirmDialog';
import { UiButton } from './common/UiButton';
import { useAuth } from '../hooks/useAuth';

interface EntityPageProps {
  config: EntityConfig;
  useStore: EntityStore;
  showRiskTags?: boolean;
  showResultPanel?: boolean;
  showDispositionGate?: boolean;
}

export function EntityPage({ config, useStore, showRiskTags = false, showResultPanel = false, showDispositionGate = false }: EntityPageProps) {
  const { items, meta, loading, error, load, createRecord, transition } = useStore();
  const { session, hasRole } = useAuth();
  const [search, setSearch] = useState('');
  const [showCreate, setShowCreate] = useState(false);
  const [pending, setPending] = useState<{ item: DomainRecord; status: string } | null>(null);

  useEffect(() => { void load(config.path); }, [config.path, load]);
  const highRisk = useMemo(() => items.filter((item) => ['high', 'critical'].includes(item.riskLevel)).length, [items]);
  const canOperate = hasRole('operator');

  // 处置事项按业务编号汇总，检测运行和结果页面共用同一闸门视图。
  const allDispositions = useMemo<CriticalDisposition[]>(() => {
    if (!showDispositionGate) return [];
    const byID = new Map<number, CriticalDisposition>();
    for (const item of items) for (const disposition of item.dispositions ?? []) byID.set(disposition.id, disposition);
    return Array.from(byID.values()).sort((a, b) => b.id - a.id);
  }, [items, showDispositionGate]);
  const pendingDispositions = useMemo(() => allDispositions.filter((d) => d.status === 'pending' && d.assayStatus === 'validated').length, [allDispositions]);

  const createDemo = async () => {
    const now = Date.now();
    await createRecord(config.path, {
      code: `${config.key.toUpperCase()}-${now.toString().slice(-6)}`,
      name: `新增${config.label}`,
      description: '通过前端工作台创建的业务记录',
      facility: '默认检验区', owner: session?.displayName || '现场操作员', category: '常规', riskLevel: 'medium',
      metricValue: 25, metricUnit: 'unit', effectiveAt: new Date().toISOString(), evidence: '已完成创建前证据核对', relatedCode: '',
    });
    setSearch('');
    setShowCreate(false);
  };

  const requiresPreparer = (item: DomainRecord) => config.key === 'resultSignoff' && item.status === 'draft';
  const requiresReviewer = (item: DomainRecord) => config.key === 'resultSignoff' && item.status === 'peer_review';
  const isOriginalPreparer = (item: DomainRecord) => Boolean(session?.username && session.username === item.preparedBy);
  const gateClosed = (item: DomainRecord) =>
    config.key === 'resultSignoff' && item.status === 'draft' && isGateBlocked(item.dispositions);
  const canAdvance = (item: DomainRecord) => canOperate
    && (!requiresPreparer(item) || isOriginalPreparer(item))
    && (!requiresReviewer(item) || (hasRole('reviewer') && !isOriginalPreparer(item)))
    && !gateClosed(item);

  const unavailableReason = (item: DomainRecord) => {
    if (!canOperate) return '只读';
    if (gateClosed(item)) return '待危急处置';
    if (requiresPreparer(item) && !isOriginalPreparer(item)) return '等待制单人';
    if (requiresReviewer(item) && !hasRole('reviewer')) return '等待复核员';
    if (requiresReviewer(item) && isOriginalPreparer(item)) return '需异人复核';
    return '流程结束';
  };

  const confirmTransition = async () => {
    if (!pending) return;
    await transition(config.path, pending.item, pending.status);
    setSearch('');
    setPending(null);
  };

  const refresh = () => void load(config.path, search);

  return <main className="workspace">
    <header className="page-header"><div><p className="eyebrow">业务工作台</p><h1>{config.label}</h1><p>统一管理{config.label}的状态、风险、证据与责任人。</p></div>{canOperate ? <UiButton onClick={() => setShowCreate(true)}>新增{config.label}</UiButton> : <span className="access-note">只读权限</span>}</header>
    <section className="metrics"><MetricCard label="记录总数" value={meta.total} detail="当前筛选范围"/><MetricCard label="高风险" value={highRisk} detail="需要优先复核"/>{showDispositionGate ? <MetricCard label="待处置事项" value={pendingDispositions} detail="危急闸门阻断中"/> : <MetricCard label="状态种类" value={new Set(items.map((item) => item.status)).size} detail="状态机覆盖"/>}</section>
    {showDispositionGate && <section className="result-section"><header><h2>危急检验结果处置闸门</h2><span>核验通过的危急结果须先确认处置事项，同业务编号结果方可进入复核</span></header><DispositionPanel dispositions={allDispositions} onChanged={refresh} /></section>}
    {showResultPanel && <section className="result-section"><header><h2>结果与版本证据</h2><span>签发版本、操作者和请求 ID 可追溯</span></header><ResultPanel records={items} /></section>}
    <section className="toolbar"><input aria-label="搜索" placeholder={`搜索${config.label}编码或名称`} value={search} onChange={(event) => setSearch(event.target.value)} /><UiButton onClick={() => void load(config.path, search)}>查询</UiButton><button className="link-button" onClick={() => { setSearch(''); void load(config.path); }}>重置</button></section>
    {error && <div className="alert" role="alert">{error}</div>}
    <section className="table-shell" aria-busy={loading}><table><thead><tr><th>编码</th><th>名称</th><th>状态</th><th>风险</th><th>责任人</th><th>指标</th><th>更新时间</th><th>操作</th></tr></thead><tbody>
      {items.map((item) => { const next = nextStatus(item.status, config.primaryTransitions); return <tr key={item.id}><td><strong>{item.code}</strong></td><td>{item.name}<small>{item.facility}</small></td><td><StatusBadge status={item.status}/>{gateClosed(item) && <div className="gate-flag">待危急处置</div>}</td><td>{showRiskTags ? <RiskTag level={item.riskLevel}/> : item.riskLevel}</td><td>{item.owner}</td><td>{item.metricValue} {item.metricUnit}</td><td>{formatDate(item.updatedAt)}</td><td>{next && canAdvance(item) ? <button className="table-action" onClick={() => setPending({ item, status: next })}>推进至 {next}</button> : <span className="muted">{next ? unavailableReason(item) : '流程结束'}</span>}</td></tr>; })}
      {!items.length && !loading && <EmptyState message="暂无记录" colSpan={8} />}
    </tbody></table>{loading && <div className="loading">正在同步业务数据…</div>}</section>
    <ConfirmDialog open={showCreate} title={`新增${config.label}`} onCancel={() => setShowCreate(false)} onConfirm={() => void createDemo().catch(() => undefined)}><p>将创建一条包含完整责任人、风险和证据信息的演示记录。</p></ConfirmDialog>
    <ConfirmDialog open={Boolean(pending)} title="确认状态迁移" onCancel={() => setPending(null)} onConfirm={() => void confirmTransition().catch(() => undefined)}><p>状态迁移会写入不可覆盖的版本与审计日志。</p><strong>{pending?.item.status} → {pending?.status}</strong></ConfirmDialog>
  </main>;
}
