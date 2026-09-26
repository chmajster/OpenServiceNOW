import { useEffect, useMemo, useState, type FormEvent } from "react";
import { ApiError, api, jsonBody } from "../api/client";
import type {
  AssignedRole,
  AssignedRolePage,
  CreatedID,
  Group,
  GroupMember,
  GroupMemberPage,
  GroupsResponse,
  Permission,
  PermissionsResponse,
  Role,
  RolesResponse,
  TenantUser,
  UsersResponse
} from "../api/types";
import { useAuth } from "../auth/AuthContext";
import { PageState } from "../components/PageState";

export function RbacPage() {
  const { hasPermission } = useAuth();
  const canManageUsers = hasPermission("admin.users.manage");

  const [permissions, setPermissions] = useState<Permission[]>([]);
  const [roles, setRoles] = useState<Role[]>([]);
  const [groups, setGroups] = useState<Group[]>([]);
  const [users, setUsers] = useState<TenantUser[]>([]);

  const [nextRoleAfterID, setNextRoleAfterID] = useState<string | undefined>();
  const [nextGroupAfterID, setNextGroupAfterID] = useState<string | undefined>();
  const [roleSearch, setRoleSearch] = useState("");
  const [activeRoleSearch, setActiveRoleSearch] = useState("");
  const [groupSearch, setGroupSearch] = useState("");
  const [activeGroupSearch, setActiveGroupSearch] = useState("");
  const [userSearch, setUserSearch] = useState("");

  const [loading, setLoading] = useState(true);
  const [loadingMoreRoles, setLoadingMoreRoles] = useState(false);
  const [loadingMoreGroups, setLoadingMoreGroups] = useState(false);
  const [searchingUsers, setSearchingUsers] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const [roleName, setRoleName] = useState("");
  const [roleDescription, setRoleDescription] = useState("");
  const [selectedPermissions, setSelectedPermissions] = useState<string[]>([]);
  const [editRoleID, setEditRoleID] = useState("");
  const [editRolePermissions, setEditRolePermissions] = useState<string[]>([]);
  const [groupName, setGroupName] = useState("");
  const [groupDescription, setGroupDescription] = useState("");
  const [assignmentUser, setAssignmentUser] = useState("");
  const [assignmentRole, setAssignmentRole] = useState("");
  const [assignedUserRoles, setAssignedUserRoles] = useState<AssignedRole[]>([]);
  const [nextUserRoleAfterID, setNextUserRoleAfterID] = useState<string | undefined>();
  const [loadingUserRoles, setLoadingUserRoles] = useState(false);

  const [assignmentGroup, setAssignmentGroup] = useState("");
  const [groupMemberUser, setGroupMemberUser] = useState("");
  const [groupRole, setGroupRole] = useState("");
  const [groupMembers, setGroupMembers] = useState<GroupMember[]>([]);
  const [assignedGroupRoles, setAssignedGroupRoles] = useState<AssignedRole[]>([]);
  const [nextGroupMemberAfterID, setNextGroupMemberAfterID] = useState<string | undefined>();
  const [nextGroupRoleAfterID, setNextGroupRoleAfterID] = useState<string | undefined>();
  const [loadingGroupMembers, setLoadingGroupMembers] = useState(false);
  const [loadingGroupRoles, setLoadingGroupRoles] = useState(false);

  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState<string | null>(null);

  const activeUsers = useMemo(() => users.filter((user) => user.membership_active), [users]);

  function listParams(afterID: string | undefined, query: string) {
    const params = new URLSearchParams({ limit: "50" });
    if (afterID) {
      params.set("after_id", afterID);
    }
    if (query.trim()) {
      params.set("q", query.trim());
    }
    return params;
  }

  async function loadRoles(afterID?: string, append = false, query = activeRoleSearch) {
    append ? setLoadingMoreRoles(true) : setLoading(true);
    setError(null);
    try {
      const response = await api<RolesResponse>(
        `/api/v1/admin/rbac/roles?${listParams(afterID, query).toString()}`
      );
      setRoles((current) => (append ? [...current, ...response.roles] : response.roles));
      setNextRoleAfterID(response.next_after_id);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Nie udało się pobrać ról.");
    } finally {
      setLoading(false);
      setLoadingMoreRoles(false);
    }
  }

  async function loadGroups(afterID?: string, append = false, query = activeGroupSearch) {
    append ? setLoadingMoreGroups(true) : setLoading(true);
    setError(null);
    try {
      const response = await api<GroupsResponse>(
        `/api/v1/admin/rbac/groups?${listParams(afterID, query).toString()}`
      );
      setGroups((current) => (append ? [...current, ...response.groups] : response.groups));
      setNextGroupAfterID(response.next_after_id);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Nie udało się pobrać grup.");
    } finally {
      setLoading(false);
      setLoadingMoreGroups(false);
    }
  }

  async function loadPermissionCatalog() {
    setError(null);
    try {
      const response = await api<PermissionsResponse>("/api/v1/admin/rbac/permissions");
      setPermissions(response.permissions);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Nie udało się pobrać katalogu uprawnień.");
    }
  }

  async function loadCore() {
    setLoading(true);
    setError(null);
    await Promise.all([
      loadPermissionCatalog(),
      loadRoles(undefined, false, activeRoleSearch),
      loadGroups(undefined, false, activeGroupSearch)
    ]);
    setLoading(false);
  }

  async function searchUsers(query = userSearch) {
    if (!canManageUsers) {
      setUsers([]);
      return;
    }
    setSearchingUsers(true);
    setError(null);
    try {
      const response = await api<UsersResponse>(
        `/api/v1/admin/users/?${listParams(undefined, query).toString()}`
      );
      setUsers(response.users);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Nie udało się wyszukać użytkowników.");
    } finally {
      setSearchingUsers(false);
    }
  }

  useEffect(() => {
    void loadCore();
  }, []);

  useEffect(() => {
    if (canManageUsers) {
      void searchUsers("");
    } else {
      setUsers([]);
    }
  }, [canManageUsers]);

  function togglePermission(key: string) {
    setSelectedPermissions((current) =>
      current.includes(key) ? current.filter((value) => value !== key) : [...current, key]
    );
  }

  function selectRoleForEdit(roleID: string) {
    setEditRoleID(roleID);
    const role = roles.find((item) => item.id === roleID);
    setEditRolePermissions(role?.permissions.map((permission) => permission.key) ?? []);
  }

  function toggleEditPermission(key: string) {
    setEditRolePermissions((current) =>
      current.includes(key) ? current.filter((value) => value !== key) : [...current, key]
    );
  }

  async function createRole(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setBusy(true);
    setError(null);
    setMessage(null);
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
      await loadRoles(undefined, false, activeRoleSearch);
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

  async function saveRolePermissions(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const role = roles.find((item) => item.id === editRoleID);
    if (!role || role.system_role) {
      return;
    }

    setBusy(true);
    setError(null);
    setMessage(null);
    try {
      await api<void>(`/api/v1/admin/rbac/roles/${role.id}/permissions`, {
        method: "PUT",
        body: jsonBody({
          version: role.version,
          permissions: editRolePermissions
        })
      });
      setMessage("Uprawnienia roli zostały zaktualizowane.");
      setEditRoleID("");
      setEditRolePermissions([]);
      await loadRoles(undefined, false, activeRoleSearch);
    } catch (cause) {
      if (cause instanceof ApiError && cause.status === 409) {
        setEditRoleID("");
        setEditRolePermissions([]);
        await loadRoles(undefined, false, activeRoleSearch);
        setError("Rola została zmieniona równolegle. Bieżące dane zostały odświeżone; wybierz rolę i ponów edycję.");
      } else {
        setError(cause instanceof Error ? cause.message : "Aktualizacja uprawnień roli nie powiodła się.");
      }
    } finally {
      setBusy(false);
    }
  }

  async function createGroup(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setBusy(true);
    setError(null);
    setMessage(null);
    try {
      await api<CreatedID>("/api/v1/admin/rbac/groups", {
        method: "POST",
        body: jsonBody({ name: groupName, description: groupDescription })
      });
      setGroupName("");
      setGroupDescription("");
      await loadGroups(undefined, false, activeGroupSearch);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Utworzenie grupy nie powiodło się.");
    } finally {
      setBusy(false);
    }
  }

  async function handleRoleSearch(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const query = roleSearch.trim();
    setActiveRoleSearch(query);
    setAssignmentRole("");
    setEditRoleID("");
    setEditRolePermissions([]);
    await loadRoles(undefined, false, query);
  }

  async function clearRoleSearch() {
    setRoleSearch("");
    setActiveRoleSearch("");
    setAssignmentRole("");
    setEditRoleID("");
    setEditRolePermissions([]);
    await loadRoles(undefined, false, "");
  }

  async function handleGroupSearch(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const query = groupSearch.trim();
    setActiveGroupSearch(query);
    await loadGroups(undefined, false, query);
  }

  async function clearGroupSearch() {
    setGroupSearch("");
    setActiveGroupSearch("");
    await loadGroups(undefined, false, "");
  }

  async function handleUserSearch(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setAssignmentUser("");
    setAssignedUserRoles([]);
    setNextUserRoleAfterID(undefined);
    setGroupMemberUser("");
    await searchUsers();
  }

  async function loadUserRoles(userID: string, afterID?: string, append = false) {
    if (!userID || !canManageUsers) {
      setAssignedUserRoles([]);
      setNextUserRoleAfterID(undefined);
      return;
    }
    setLoadingUserRoles(true);
    setError(null);
    try {
      const response = await api<AssignedRolePage>(
        `/api/v1/admin/rbac/users/${userID}/roles?${listParams(afterID, "").toString()}`
      );
      setAssignedUserRoles((current) => append ? [...current, ...response.roles] : response.roles);
      setNextUserRoleAfterID(response.next_after_id);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Nie udało się pobrać ról użytkownika.");
    } finally {
      setLoadingUserRoles(false);
    }
  }

  async function selectAssignmentUser(userID: string) {
    setAssignmentUser(userID);
    setAssignmentRole("");
    await loadUserRoles(userID);
  }

  async function loadGroupMembers(groupID: string, afterID?: string, append = false) {
    if (!groupID || !canManageUsers) {
      setGroupMembers([]);
      setNextGroupMemberAfterID(undefined);
      return;
    }
    setLoadingGroupMembers(true);
    setError(null);
    try {
      const response = await api<GroupMemberPage>(
        `/api/v1/admin/rbac/groups/${groupID}/members?${listParams(afterID, "").toString()}`
      );
      setGroupMembers((current) => append ? [...current, ...response.members] : response.members);
      setNextGroupMemberAfterID(response.next_after_id);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Nie udało się pobrać członków grupy.");
    } finally {
      setLoadingGroupMembers(false);
    }
  }

  async function loadGroupRoles(groupID: string, afterID?: string, append = false) {
    if (!groupID) {
      setAssignedGroupRoles([]);
      setNextGroupRoleAfterID(undefined);
      return;
    }
    setLoadingGroupRoles(true);
    setError(null);
    try {
      const response = await api<AssignedRolePage>(
        `/api/v1/admin/rbac/groups/${groupID}/roles?${listParams(afterID, "").toString()}`
      );
      setAssignedGroupRoles((current) => append ? [...current, ...response.roles] : response.roles);
      setNextGroupRoleAfterID(response.next_after_id);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Nie udało się pobrać ról grupy.");
    } finally {
      setLoadingGroupRoles(false);
    }
  }

  async function selectAssignmentGroup(groupID: string) {
    setAssignmentGroup(groupID);
    setGroupMemberUser("");
    setGroupRole("");
    setGroupMembers([]);
    setAssignedGroupRoles([]);
    setNextGroupMemberAfterID(undefined);
    setNextGroupRoleAfterID(undefined);
    if (!groupID) {
      return;
    }
    await Promise.all([
      loadGroupRoles(groupID),
      canManageUsers ? loadGroupMembers(groupID) : Promise.resolve()
    ]);
  }

  async function assignRole(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const userID = assignmentUser;
    const roleID = assignmentRole;
    if (!userID || !roleID) {
      return;
    }
    setBusy(true);
    setError(null);
    setMessage(null);
    try {
      await api<void>(`/api/v1/admin/rbac/users/${userID}/roles/${roleID}`, { method: "POST" });
      setAssignmentRole("");
      await loadUserRoles(userID);
      setMessage("Rola została przypisana użytkownikowi.");
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Przypisanie roli nie powiodło się.");
    } finally {
      setBusy(false);
    }
  }

  async function removeUserRole(roleID: string) {
    if (!assignmentUser) {
      return;
    }
    setBusy(true);
    setError(null);
    setMessage(null);
    try {
      await api<void>(`/api/v1/admin/rbac/users/${assignmentUser}/roles/${roleID}`, { method: "DELETE" });
      await loadUserRoles(assignmentUser);
      setMessage("Rola została usunięta z użytkownika.");
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Usunięcie roli użytkownika nie powiodło się.");
    } finally {
      setBusy(false);
    }
  }

  async function addGroupMember(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!assignmentGroup || !groupMemberUser) {
      return;
    }
    setBusy(true);
    setError(null);
    setMessage(null);
    try {
      await api<void>(`/api/v1/admin/rbac/groups/${assignmentGroup}/members/${groupMemberUser}`, { method: "POST" });
      setGroupMemberUser("");
      await Promise.all([
        loadGroupMembers(assignmentGroup),
        loadGroups(undefined, false, activeGroupSearch)
      ]);
      setMessage("Użytkownik został dodany do grupy.");
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Dodanie użytkownika do grupy nie powiodło się.");
    } finally {
      setBusy(false);
    }
  }

  async function removeGroupMember(userID: string) {
    if (!assignmentGroup) {
      return;
    }
    setBusy(true);
    setError(null);
    setMessage(null);
    try {
      await api<void>(`/api/v1/admin/rbac/groups/${assignmentGroup}/members/${userID}`, { method: "DELETE" });
      await Promise.all([
        loadGroupMembers(assignmentGroup),
        loadGroups(undefined, false, activeGroupSearch)
      ]);
      setMessage("Użytkownik został usunięty z grupy.");
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Usunięcie użytkownika z grupy nie powiodło się.");
    } finally {
      setBusy(false);
    }
  }

  async function assignGroupRole(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!assignmentGroup || !groupRole) {
      return;
    }
    setBusy(true);
    setError(null);
    setMessage(null);
    try {
      await api<void>(`/api/v1/admin/rbac/groups/${assignmentGroup}/roles/${groupRole}`, { method: "POST" });
      setGroupRole("");
      await loadGroupRoles(assignmentGroup);
      setMessage("Rola została przypisana grupie.");
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Przypisanie roli grupie nie powiodło się.");
    } finally {
      setBusy(false);
    }
  }

  async function removeGroupRole(roleID: string) {
    if (!assignmentGroup) {
      return;
    }
    setBusy(true);
    setError(null);
    setMessage(null);
    try {
      await api<void>(`/api/v1/admin/rbac/groups/${assignmentGroup}/roles/${roleID}`, { method: "DELETE" });
      await loadGroupRoles(assignmentGroup);
      setMessage("Rola została usunięta z grupy.");
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Usunięcie roli z grupy nie powiodło się.");
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
        <button className="button button--secondary" onClick={() => void loadCore()} disabled={loading}>
          Odśwież
        </button>
      </div>

      {error ? <div className="inline-error" role="alert">{error}</div> : null}
      {message ? <div className="inline-success" role="status">{message}</div> : null}

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

          <hr />

          <div className="panel-heading"><h2>Edytuj uprawnienia roli</h2></div>
          <form className="form-stack form-stack--compact" onSubmit={(event) => void saveRolePermissions(event)}>
            <label>
              Rola do edycji
              <select value={editRoleID} onChange={(event) => selectRoleForEdit(event.target.value)}>
                <option value="">Wybierz niesystemową rolę z załadowanych wyników</option>
                {roles.filter((role) => !role.system_role).map((role) => (
                  <option key={role.id} value={role.id}>{role.name} — v{role.version}</option>
                ))}
              </select>
            </label>
            {editRoleID ? (
              <fieldset className="permission-grid">
                <legend>Uprawnienia po zmianie</legend>
                {permissions.map((permission) => (
                  <label className="checkbox-row" key={permission.key}>
                    <input
                      type="checkbox"
                      checked={editRolePermissions.includes(permission.key)}
                      onChange={() => toggleEditPermission(permission.key)}
                    />
                    <span>
                      <strong>{permission.key}</strong>
                      <small>{permission.description}</small>
                    </span>
                  </label>
                ))}
              </fieldset>
            ) : null}
            <button className="button button--secondary" type="submit" disabled={busy || !editRoleID}>
              Zapisz uprawnienia
            </button>
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
          {canManageUsers ? (
            <>
              <form className="search-row search-row--compact" onSubmit={(event) => void handleUserSearch(event)}>
                <label className="search-field">
                  Wyszukaj użytkownika
                  <input
                    value={userSearch}
                    onChange={(event) => setUserSearch(event.target.value)}
                    maxLength={200}
                    placeholder="nazwa lub e-mail"
                  />
                </label>
                <button className="button button--secondary" type="submit" disabled={searchingUsers}>
                  {searchingUsers ? "Szukanie…" : "Szukaj"}
                </button>
              </form>
              <form className="form-stack form-stack--compact" onSubmit={(event) => void assignRole(event)}>
                <label>
                  Użytkownik
                  <select value={assignmentUser} onChange={(event) => void selectAssignmentUser(event.target.value)} required>
                    <option value="">Wybierz z maks. 50 wyników</option>
                    {activeUsers.map((user) => (
                      <option key={user.id} value={user.id}>{user.display_name} — {user.email}</option>
                    ))}
                  </select>
                </label>
                <label>
                  Rola do przypisania
                  <select value={assignmentRole} onChange={(event) => setAssignmentRole(event.target.value)} required>
                    <option value="">Wybierz z załadowanych ról</option>
                    {roles.map((role) => (
                      <option key={role.id} value={role.id}>{role.name}</option>
                    ))}
                  </select>
                </label>
                <button className="button button--secondary" type="submit" disabled={busy}>Przypisz rolę</button>
              </form>
              {assignmentUser ? (
                <div className="assignment-list">
                  <h3>Role przypisane bezpośrednio</h3>
                  {assignedUserRoles.map((role) => (
                    <div className="assignment-row" key={role.id}>
                      <span>
                        <strong>{role.name}</strong>
                        {role.system_role ? <small className="muted">rola systemowa</small> : null}
                      </span>
                      <button
                        className="button button--small button--secondary"
                        type="button"
                        disabled={busy}
                        onClick={() => void removeUserRole(role.id)}
                      >
                        Usuń
                      </button>
                    </div>
                  ))}
                  {assignedUserRoles.length === 0 && !loadingUserRoles ? (
                    <div className="empty-cell">Brak bezpośrednio przypisanych ról.</div>
                  ) : null}
                  {nextUserRoleAfterID ? (
                    <button
                      className="button button--secondary"
                      type="button"
                      disabled={loadingUserRoles}
                      onClick={() => void loadUserRoles(assignmentUser, nextUserRoleAfterID, true)}
                    >
                      {loadingUserRoles ? "Ładowanie…" : "Załaduj więcej ról użytkownika"}
                    </button>
                  ) : null}
                </div>
              ) : null}
            </>
          ) : (
            <PageState
              title="Lista użytkowników niedostępna"
              detail="Przypisywanie roli użytkownikowi z tego widoku wymaga również admin.users.manage."
            />
          )}
        </div>
      </div>

      <div className="panel">
        <div className="panel-heading">
          <div>
            <h2>Przypisania grupowe</h2>
            <p className="muted">Po każdej zmianie stan jest ponownie pobierany z backendu.</p>
          </div>
        </div>
        <label className="search-field">
          Grupa
          <select value={assignmentGroup} onChange={(event) => void selectAssignmentGroup(event.target.value)}>
            <option value="">Wybierz grupę z załadowanych wyników</option>
            {groups.map((group) => (
              <option key={group.id} value={group.id}>{group.name}</option>
            ))}
          </select>
        </label>

        {assignmentGroup ? (
          <div className="two-column assignment-columns">
            <section className="assignment-section">
              <h3>Członkowie grupy</h3>
              {canManageUsers ? (
                <>
                  <form className="form-stack form-stack--compact" onSubmit={(event) => void addGroupMember(event)}>
                    <label>
                      Użytkownik
                      <select value={groupMemberUser} onChange={(event) => setGroupMemberUser(event.target.value)} required>
                        <option value="">Wybierz aktywnego użytkownika</option>
                        {activeUsers.map((user) => (
                          <option key={user.id} value={user.id}>{user.display_name} — {user.email}</option>
                        ))}
                      </select>
                    </label>
                    <button className="button button--secondary" type="submit" disabled={busy}>Dodaj do grupy</button>
                  </form>
                  <div className="assignment-list">
                    {groupMembers.map((member) => (
                      <div className="assignment-row" key={member.user_id}>
                        <span>
                          <strong>{member.display_name}</strong>
                          <small className="muted">{member.email}</small>
                        </span>
                        <button
                          className="button button--small button--secondary"
                          type="button"
                          disabled={busy}
                          onClick={() => void removeGroupMember(member.user_id)}
                        >
                          Usuń
                        </button>
                      </div>
                    ))}
                    {groupMembers.length === 0 && !loadingGroupMembers ? (
                      <div className="empty-cell">Grupa nie ma członków.</div>
                    ) : null}
                    {nextGroupMemberAfterID ? (
                      <button
                        className="button button--secondary"
                        type="button"
                        disabled={loadingGroupMembers}
                        onClick={() => void loadGroupMembers(assignmentGroup, nextGroupMemberAfterID, true)}
                      >
                        {loadingGroupMembers ? "Ładowanie…" : "Załaduj więcej członków"}
                      </button>
                    ) : null}
                  </div>
                </>
              ) : (
                <PageState
                  title="Członkowie niedostępni"
                  detail="Odczyt i wybór użytkowników wymaga również admin.users.manage."
                />
              )}
            </section>

            <section className="assignment-section">
              <h3>Role grupy</h3>
              <form className="form-stack form-stack--compact" onSubmit={(event) => void assignGroupRole(event)}>
                <label>
                  Rola
                  <select value={groupRole} onChange={(event) => setGroupRole(event.target.value)} required>
                    <option value="">Wybierz rolę z załadowanych wyników</option>
                    {roles.map((role) => (
                      <option key={role.id} value={role.id}>{role.name}</option>
                    ))}
                  </select>
                </label>
                <button className="button button--secondary" type="submit" disabled={busy}>Przypisz rolę grupie</button>
              </form>
              <div className="assignment-list">
                {assignedGroupRoles.map((role) => (
                  <div className="assignment-row" key={role.id}>
                    <span>
                      <strong>{role.name}</strong>
                      {role.system_role ? <small className="muted">rola systemowa</small> : null}
                    </span>
                    <button
                      className="button button--small button--secondary"
                      type="button"
                      disabled={busy}
                      onClick={() => void removeGroupRole(role.id)}
                    >
                      Usuń
                    </button>
                  </div>
                ))}
                {assignedGroupRoles.length === 0 && !loadingGroupRoles ? (
                  <div className="empty-cell">Grupa nie ma przypisanych ról.</div>
                ) : null}
                {nextGroupRoleAfterID ? (
                  <button
                    className="button button--secondary"
                    type="button"
                    disabled={loadingGroupRoles}
                    onClick={() => void loadGroupRoles(assignmentGroup, nextGroupRoleAfterID, true)}
                  >
                    {loadingGroupRoles ? "Ładowanie…" : "Załaduj więcej ról grupy"}
                  </button>
                ) : null}
              </div>
            </section>
          </div>
        ) : (
          <div className="empty-cell">Wybierz grupę, aby zarządzać członkostwem i rolami.</div>
        )}
      </div>

      <div className="panel">
        <div className="panel-heading">
          <div>
            <h2>Role</h2>
            <p className="muted">{roles.length} załadowanych rekordów{activeRoleSearch ? ` dla „${activeRoleSearch}”` : ""}</p>
          </div>
        </div>
        <form className="search-row" onSubmit={(event) => void handleRoleSearch(event)}>
          <label className="search-field">
            Szukaj roli
            <input value={roleSearch} onChange={(event) => setRoleSearch(event.target.value)} maxLength={200} />
          </label>
          <button className="button button--secondary" type="submit" disabled={loading}>Szukaj</button>
          {activeRoleSearch ? (
            <button className="button button--secondary" type="button" onClick={() => void clearRoleSearch()} disabled={loading}>
              Wyczyść
            </button>
          ) : null}
        </form>
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
          {roles.length === 0 ? <div className="empty-cell">Brak ról dla bieżącego filtra.</div> : null}
        </div>
        {nextRoleAfterID ? (
          <div className="panel-footer">
            <button
              className="button button--secondary"
              disabled={loadingMoreRoles}
              onClick={() => void loadRoles(nextRoleAfterID, true, activeRoleSearch)}
            >
              {loadingMoreRoles ? "Ładowanie…" : "Załaduj więcej ról"}
            </button>
          </div>
        ) : null}
      </div>

      <div className="panel">
        <div className="panel-heading">
          <div>
            <h2>Grupy</h2>
            <p className="muted">{groups.length} załadowanych rekordów{activeGroupSearch ? ` dla „${activeGroupSearch}”` : ""}</p>
          </div>
        </div>
        <form className="search-row" onSubmit={(event) => void handleGroupSearch(event)}>
          <label className="search-field">
            Szukaj grupy
            <input value={groupSearch} onChange={(event) => setGroupSearch(event.target.value)} maxLength={200} />
          </label>
          <button className="button button--secondary" type="submit" disabled={loading}>Szukaj</button>
          {activeGroupSearch ? (
            <button className="button button--secondary" type="button" onClick={() => void clearGroupSearch()} disabled={loading}>
              Wyczyść
            </button>
          ) : null}
        </form>
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
          {groups.length === 0 ? <div className="empty-cell">Brak grup dla bieżącego filtra.</div> : null}
        </div>
        {nextGroupAfterID ? (
          <div className="panel-footer">
            <button
              className="button button--secondary"
              disabled={loadingMoreGroups}
              onClick={() => void loadGroups(nextGroupAfterID, true, activeGroupSearch)}
            >
              {loadingMoreGroups ? "Ładowanie…" : "Załaduj więcej grup"}
            </button>
          </div>
        ) : null}
      </div>
    </section>
  );
}
