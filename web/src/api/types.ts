export type ApiErrorBody = {
  error: {
    code: string;
    message: string;
    request_id?: string;
  };
};

export type AuthUser = {
  id: string;
  email: string;
  display_name: string;
  version: string;
  credential_version: string;
};

export type AuthTenant = {
  id: string;
  slug: string;
  name: string;
};

export type AuthMe = {
  password_change_required: boolean;
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

export type SetupStatus = {
  required: boolean;
  token?: string;
};

export type SetupResult = {
  tenant: string;
  email: string;
};

export type Tenant = {
  id: string;
  slug: string;
  name: string;
  status: "ACTIVE" | "DISABLED";
  version: string;
  created_at: string;
  updated_at: string;
};

export type TenantResponse = {
  tenant: Tenant;
};

export type TenantUser = {
  id: string;
  email: string;
  display_name: string;
  user_active: boolean;
  membership_active: boolean;
  created_at: string;
};

export type UsersResponse = {
  users: TenantUser[];
  next_after_id?: string;
};

export type Permission = {
  id: string;
  key: string;
  description?: string;
};

export type Role = {
  id: string;
  name: string;
  description?: string;
  system_role: boolean;
  version: string;
  permissions: Permission[];
};

export type RolesResponse = {
  roles: Role[];
  next_after_id?: string;
};

export type Group = {
  id: string;
  name: string;
  description?: string;
  version: string;
  member_count: string;
};

export type GroupsResponse = {
  groups: Group[];
  next_after_id?: string;
};

export type PermissionsResponse = {
  permissions: Permission[];
};

export type AuditRecord = {
  id: string;
  actor_user_id?: string;
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
  next_before_id?: string;
};

export type CreatedID = {
  id: string;
};

export type HealthResponse = {
  status: "ok" | "unavailable";
  checks?: Record<string, string>;
};

export type GroupMember = {
  user_id: string;
  email: string;
  display_name: string;
  user_active: boolean;
  membership_active: boolean;
  joined_at: string;
};

export type GroupMemberPage = {
  members: GroupMember[];
  next_after_id?: string;
};

export type AssignedRole = {
  id: string;
  name: string;
  system_role: boolean;
  assigned_at: string;
};

export type AssignedRolePage = {
  roles: AssignedRole[];
  next_after_id?: string;
};
