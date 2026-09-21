import type { DispositionState } from '../types/status';

export const DISPOSITION_LABELS: Record<DispositionState, string> = {
  pending: '待处置',
  confirmed: '已确认处置',
  void: '已失效',
};

export const DISPOSITION_TONES: Record<DispositionState, 'success' | 'warning' | 'danger' | 'neutral'> = {
  pending: 'warning',
  confirmed: 'success',
  void: 'danger',
};

export function dispositionLabel(status?: string | null): string {
  return DISPOSITION_LABELS[(status || '') as DispositionState] || status || '无处置事项';
}

export function dispositionTone(status?: string | null): 'success' | 'warning' | 'danger' | 'neutral' {
  return DISPOSITION_TONES[(status || '') as DispositionState] || 'neutral';
}
