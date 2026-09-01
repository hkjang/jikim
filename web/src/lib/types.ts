export type Role = 'admin' | 'manager' | 'user' | 'auditor';

export interface User {
  id: string;
  username: string;
  display_name?: string;
  email?: string;
  role: Role | string;
  status?: string;
  last_login_at?: string;
  created_at?: string;
}

export interface SecretRecord {
  id: string;
  path: string;
  name?: string;
  owner?: string;
  application?: string;
  environment?: string;
  tags?: string[];
  risk_score?: number;
  version?: number;
  status?: string;
  last_accessed_at?: string;
  updated_at?: string;
  created_at?: string;
  data?: Record<string, unknown>;
  metadata?: Record<string, unknown>;
}

export interface ApplicationRecord {
  id: string;
  name: string;
  owner?: string;
  criticality?: string;
  environment?: string;
  repository?: string;
  secret_count?: number;
  status?: string;
  updated_at?: string;
}

export interface ApprovalRecord {
  id: string;
  action?: string;
  type?: string;
  resource?: string;
  requester_id?: string;
  requester?: string;
  reason?: string;
  status: string;
  created_at?: string;
  expires_at?: string;
}

export interface AuditRecord {
  id: string;
  actor?: string;
  action: string;
  resource?: string;
  ip?: string;
  result?: string;
  created_at?: string;
  metadata?: Record<string, unknown>;
}

export interface KeyRecord {
  id: string;
  name: string;
  type?: string;
  algorithm?: string;
  version?: number;
  status?: string;
  owner?: string;
  owner_user_id?: string;
  permissions?: Record<string, boolean>;
  rotated_at?: string;
  next_rotation_at?: string;
  created_at?: string;
}

export interface DashboardData {
  secrets?: number;
  applications?: number;
  users?: number;
  keys?: number;
  high_risk?: number;
  expiring?: number;
  rotation_failed?: number;
  pending_approvals?: number;
  security_score?: number;
  recent_audit?: AuditRecord[];
  risk_distribution?: Array<{ name: string; value: number }>;
}

export interface SystemSettings {
  general?: Record<string, unknown>;
  approval?: { enabled?: boolean; reviewer_role?: string; four_eyes?: boolean; required_approvals?: number };
  oidc?: Record<string, unknown>;
  ai?: Record<string, unknown>;
  security?: Record<string, unknown>;
  notifications?: Record<string, unknown>;
  [key: string]: unknown;
}
