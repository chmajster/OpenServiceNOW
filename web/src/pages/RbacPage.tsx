import { useEffect, useMemo, useState, type FormEvent } from "react";
import { ApiError, api, jsonBody } from "../api/client";
import type {
  CreatedID,
  Group,
  GroupsResponse,
  Permission,
  PermissionsResponse,
  Role,
  RolesResponse,
  TenantUser,
  UsersResponse
} from "../api/types";
import { PageState } from "../components/PageState";

export function RbacPage() {
  const [permissions, setPermissions] = useState<Permission[]>([]);
  const [roles, setRoles] = useState<Role[]>([]);
  const [groups, setGroups] = useState<Group[]>([]);
  const [users, setUsers] = useState<TenantUser[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const [roleName, setRoleName] = useState("");
  const [roleDescription, setRoleDescription] = useState("");
  const [selectedPermissions, setSelectedPermissions] = useState<string[]>([]);
  const [groupName, setGroupName] = useState("");
  const [groupDescription, setGroupDescription] = useState("");
  const [assignmentUser, setAssignmentUser] = useState("");
  const [assignmentRole, setAssignmentRole] = useState("");
  const [busy, setBusy] = useState(false);

  const activeUsers = useMemo(() => users.filter((user) => user.membership_active), [users]);

  async function load() {
    setLoading(true);
    try {
      const [permissionResponse, roleResponse, groupResponse, userResponse] = await Promise.all([
        api<PermissionsResponse>("/api/v1/admin/rbac/permissions"),
        api<RolesResponse>("/api/v1/admin/rbac/roles"),
        api<GroupsResponse>("/api/v1/admin/rbac/groups"),
        api<UsersResponse>("/api/v1/admin/users/")
      ]);
      setPermissions(permissionResponse.permissions);
      setRoles(roleResponse.roles);
      setGroups(groupResponse.groups);
      setUsers(userResponse.users);
      setError(null);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Nie udało się pobrać konfiguracji RBAC.");
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void load();
  }, []);

  function togglePermission(key: string) {
    setSelectedPermissions((current) =>
      current.includes(key) ? current.filter((value) => value !== key) : [...current, key]
    );
  }

  async function createRole(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setBusy(true);
    setError(null);
    try {
      await api<CreatedID>("/api/v1/admin/rbac/roles", {
        method: "POST",
        body: jsonBody({
          name: roleName,
          description: roleDescription,
          permissions: selectedPermissions
        })
      });
      setRoleName("");
      setRoleDescription("");
      setSelectedPermissions([]);
      await load();
    } catch (cause) {
      if (cause instanceof ApiError && cause.status === 409) {
        setError("Rola o tej nazwie już istnieje.");
      } else {
        setError(cause instanceof Error ? cause.message : "Utworzenie roli nie powiodło się.");
      }
    } finally {
      setBusy(false);
    }
  }

  async function createGroup(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setBusy(true);
    setError(null);
    try {
      await api<CreatedID>("/api/v1/admin/rbac/groups", {
        method: "POST",
        body: jsonBody({ name: groupName, description: groupDescription })
      });
      setGroupName("");
      setGroupDescription("");
      await load();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Utworzenie grupy nie powiodło się.");
    } finally {
      setBusy(false);
    }
  }

  async function assignRole(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const userID = Number(assignmentUser);
    const roleID = Number(assignmentRole);
    if (!userID || !roleID) {
      return;
    }
    setBusy(true);
    setError(null);
    try {
      await api<void>(`/api/v1/admin/rbac/users/${userID}/roles/${roleID}`, { method: "POST" });
      setAssignmentUser("");
      setAssignmentRole("");
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Przypisanie roli nie powiodło się.");
    } finally {
      setBusy(false);
    }
  }

  if (loading && roles.length === 0 && groups.length === 0) {
    return <PageState title="Ładowanie RBAC" />;
  }

  return (
    <section className="page">
      <div className="page-heading">
        <div>
          <p className="eyebrow">Administracja</p>
          <h1>Role i RBAC</h1>
          <p className="muted">Uprawnienia są zawsze egzekwowane przez backend.</p>
        </div>
        <button className="button button--secondary" onClick={() => void load()} disabled={loading}>Odśwież</button>
      </div>

      {error ? <div className="inline-error" role="alert">{error}</div> : null}

      <div className="two-column">
        <div className="panel">
          <div className="panel-heading"><h2>Nowa rola</h2></div>
          <form className="form-stack form-stack--compact" onSubmit={(event) => void createRole(event)}>
            <label>
              Nazwa
              <input value={roleName} onChange={(event) => setRoleName(event.target.value)} maxLength={150} required />
            </label>
            <label>
              Opis
              <textarea value={roleDescription} onChange={(event) => setRoleDescription(event.target.value)} maxLength={500} rows={3} />
            </label>
            <fieldset className="permission-grid">
              <legend>Uprawnienia</legend>
              {permissions.map((permission) => (
                <label className="checkbox-row" key={permission.key}>
                  <input
                    type="checkbox"
                    checked={selectedPermissions.includes(permission.key)}
                    onChange={() => togglePermission(permission.key)}
                  />
                  <span>
                    <strong>{permission.key}</strong>
                    <small>{permission.description}</small>
                  </span>
                </label>
              ))}
            </fieldset>
            <button className="button button--primary" type="submit" disabled={busy}>Utwórz rolę</button>
          </form>
        </div>

        <div className="panel">
          <div className="panel-heading"><h2>Nowa grupa</h2></div>
          <form className="form-stack form-stack--compact" onSubmit={(event) => void createGroup(event)}>
            <label>
              Nazwa
              <input value={groupName} onChange={(event) => setGroupName(event.target.value)} maxLength={150} required />
            </label>
            <label>
              Opis
              <textarea value={groupDescription} onChange={(event) => setGroupDescription(event.target.value)} maxLength={500} rows={3} />
            </label>
            <button className="button button--primary" type="submit" disabled={busy}>Utwórz grupę</button>
          </form>

          <hr />

          <div className="panel-heading"><h2>Przypisz rolę użytkownikowi</h2></div>
          <form className="form-stack form-stack--compact" onSubmit={(event) => void assignRole(event)}>
            <label>
              Użytkownik
              <select value={assignmentUser} onChange={(event) => setAssignmentUser(event.target.value)} required>
                <option value="">Wybierz użytkownika</option>
                {activeUsers.map((user) => (
                  <option key={user.id} value={user.id}>{user.display_name} — {user.email}</option>
                ))}
              </select>
            </label>
            <label>
              Rola
              <select value={assignmentRole} onChange={(event) => setAssignmentRole(event.target.value)} required>
                <option value="">Wybierz rolę</option>
                {roles.map((role) => (
                  <option key={role.id} value={role.id}>{role.name}</option>
                ))}
              </select>
            </label>
            <button className="button button--secondary" type="submit" disabled={busy}>Przypisz rolę</button>
          </form>
        </div>
      </div>

      <div className="panel">
        <div className="panel-heading">
          <div><h2>Role</h2><p className="muted">{roles.length} rekordów</p></div>
        </div>
        <div className="card-list">
          {roles.map((role) => (
            <article className="list-card" key={role.id}>
              <div>
                <strong>{role.name}</strong>
                {role.system_role ? <span className="badge">Systemowa</span> : null}
                <p>{role.description || "Bez opisu"}</p>
              </div>
              <div className="tag-list">
                {role.permissions.map((permission) => <span className="tag" key={permission.key}>{permission.key}</span>)}
                {role.permissions.length === 0 ? <span className="muted">Brak uprawnień</span> : null}
              </div>
            </article>
          ))}
        </div>
      </div>

      <div className="panel">
        <div className="panel-heading">
          <div><h2>Grupy</h2><p className="muted">{groups.length} rekordów</p></div>
        </div>
        <div className="card-list">
          {groups.map((group) => (
            <article className="list-card" key={group.id}>
              <div>
                <strong>{group.name}</strong>
                <p>{group.description || "Bez opisu"}</p>
              </div>
              <span className="badge">{group.member_count} członków</span>
            </article>
          ))}
          {groups.length === 0 ? <div className="empty-cell">Brak grup.</div> : null}
        </div>
      </div>
    </section>
  );
}
