
import { request } from './client';
import type { DispositionItem, PageMeta } from '../types/domain';

export interface DispositionPage {
  items: DispositionItem[];
  meta: PageMeta;
}

export async function listDispositions(page = 1, pageSize = 50, status = '', search = '') {
  const params = new URLSearchParams({ page: String(page), pageSize: String(pageSize) });
  if (status) params.set('status', status);
  if (search) params.set('search', search);
  const result = await request<DispositionItem[]>(`/dispositions?${params.toString()}`);
  return { items: result.data, meta: result.meta } as DispositionPage;
}

export async function confirmDisposition(id: number, expectedVersion: number, recipient: string, measure: string) {
  return request<DispositionItem>(`/dispositions/${id}/confirm`, {
    method: 'POST',
    body: JSON.stringify({ expectedVersion, recipient, measure }),
  });
}
