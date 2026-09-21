import type { CriticalDisposition } from '../types/domain';

// isGateBlocked mirrors the backend HasBlockingGate rule using the disposition
// snapshot plus the joined current assay run status:
// - pending disposition on a validated run  -> awaiting handling, gate closed
// - voided disposition on an invalid run    -> prior confirmation no longer applies
// A confirmed disposition on a validated run keeps the gate open.
export function isGateBlocked(dispositions: CriticalDisposition[] | undefined): boolean {
  if (!dispositions?.length) return false;
  return dispositions.some((item) => {
    if (item.status === 'pending' && item.assayStatus === 'validated') return true;
    if (item.status === 'voided' && item.assayStatus === 'invalid') return true;
    return false;
  });
}

export function dispositionStatusLabel(status: string): string {
  if (status === 'pending') return '待处置';
  if (status === 'confirmed') return '已确认';
  if (status === 'voided') return '已失效';
  return status;
}
