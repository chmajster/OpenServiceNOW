export type ApiErrorBody = {
  error: {
    code: string;
    message: string;
    request_id?: string;
  };
};

export type AuthUser = {
  id: number;
  email: string;
  display_name: string;
};

export type AuthTenant = {
  id: number;
  slug: string;
  name: string;
};

export type AuthMe = {
  user: AuthUser;
  tenant: AuthTenant;
  permissions: string[];
  expires_at: string;
};

export type LoginRequest = {
  tenant: string;
  email: string;
  password: string;
};

export type Tenant = {
  id: number;
  slug: string;
  name: string;
  status: "ACTIVE" | "DISABLED";
  version: number;
  created_at: string;
  updated_at: string;
};

export type TenantResponse = {
  tenant: Tenant;
};

export type TenantUser = {
  id: number;
  email: string;
  display_name: string;
  user_active: boolean;
  membership_active: boolean;
  created_at: string;
};

export type UsersResponse = {
  users: TenantUser[];
};

export type Permission = {
  id: number;
  key: string;
  description?: string;
};

export type Role = {
  id: number;
  name: string;
  description?: string;
  system_role: boolean;
  version: number;
  permissions: Permission[];
};

export type RolesResponse = {
  roles: Role[];
};

export type Group = {
  id: number;
  name: string;
  description?: string;
  version: number;
  member_count: number;
};

export type GroupsResponse = {
  groups: Group[];
};

export type PermissionsResponse = {
  permissions: Permission[];
};

export type AuditRecord = {
  id: number;
  actor_user_id?: number;
  time: string;
  action: string;
  object_type: string;
  object_id?: string;
  old_values?: unknown;
  new_values?: unknown;
  request_id?: string;
  source: string;
  ip_address?: string;
};

export type AuditPage = {
  events: AuditRecord[];
  next_before_id?: number;
};

export type CreatedID = {
  id: number;
};

export type HealthResponse = {
  status: "ok" | "unavailable";
  checks?: Record<string, string>;
};
