
export interface DomainRecord {
  id: number;
  code: string;
  name: string;
  status: string;
  version: number;
  description: string;
  facility: string;
  owner: string;
  category: string;
  riskLevel: 'low' | 'medium' | 'high' | 'critical';
  metricValue: number;
  metricUnit: string;
  effectiveAt: string;
  evidence: string;
  relatedCode: string;
  preparedBy?: string;
  reviewedBy?: string;
  reviewReason?: string;
  revisions?: SignoffRevision[];
  operatedBy?: string;
  dispositions?: CriticalDisposition[];
  createdAt: string;
  updatedAt: string;
}

export interface CriticalDisposition {
  id: number;
  code: string;
  status: 'pending' | 'confirmed' | 'voided';
  version: number;
  relatedCode: string;
  assayRunId: number;
  assayCode: string;
  riskLevel: 'low' | 'medium' | 'high' | 'critical';
  runOperator: string;
  confirmedBy?: string;
  receiveTarget?: string;
  dispositionAction?: string;
  confirmReason?: string;
  confirmedAt?: string | null;
  voidedBy?: string;
  voidedReason?: string;
  voidedAt?: string | null;
  assayStatus?: string;
  createdAt: string;
  updatedAt: string;
}

export interface SignoffRevision {
  id: number;
  version: number;
  status: string;
  evidence: string;
  actor: string;
  requestId: string;
  action: string;
  reason: string;
  createdAt: string;
}

export interface PageMeta { page: number; pageSize: number; total: number }
export interface ApiEnvelope<T> { data: T; error?: string; message?: string; meta?: PageMeta }
export interface UserSession { token: string; username: string; displayName: string; role: string; expiresIn: number }
export interface AuditLog {
  id: number; requestId: string; actor: string; action: string; entityType: string;
  entityId: number; beforeState: string; afterState: string; detail: string; createdAt: string;
}
export interface EntityConfig {
  key: string;
  path: string;
  label: string;
  statuses: readonly string[];
  primaryTransitions: Readonly<Record<string, string>>;
}
