
import { request } from './client';
import type { CriticalDisposition } from '../types/domain';

export interface ConfirmDispositionInput {
  expectedVersion: number;
  receiveTarget: string;
  dispositionAction: string;
  reason: string;
}

export async function listDispositions(page = 1, pageSize = 50, status = '') {
  const suffix = status ? `&status=${encodeURIComponent(status)}` : '';
  return request<CriticalDisposition[]>(`/dispositions?page=${page}&pageSize=${pageSize}${suffix}`);
}

export async function confirmDisposition(id: number, input: ConfirmDispositionInput) {
  return request<CriticalDisposition>(`/dispositions/${id}/confirm`, {
    method: 'POST', body: JSON.stringify(input),
  });
}
