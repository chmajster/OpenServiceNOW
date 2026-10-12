import { useEffect, useState, type FormEvent } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { ApiError, api, jsonBody } from "../api/client";
import type { CreatedID, Group, Permission, Role } from "../api/types";
import { useAuth } from "../auth/AuthContext";
import { ConflictNotice } from "../components/ConflictNotice";
import { PageState } from "../components/PageState";
import { RemoteCombobox, type Choice } from "../components/RemoteCombobox";
import { useRemotePage } from "../components/useRemotePage";
import { GroupMembers, RoleAssignments } from "../components/RbacAssignments";
import { permissionLabel } from "../components/auditPresentation";

const base = "/api/v1/admin/rbac";
export function RbacPage() {
  const [params, setParams] = useSearchParams();
  const kind = params.get("kind") === "groups" ? "groups" : "roles";
  const id = params.get("id") ?? "";
  const query = params.get("q") ?? "";
  const creating = params.get("create") === "1";
  const [search, setSearch] = useState(query);
  useEffect(() => setSearch(query), [query]);
  const page = useRemotePage<Role | Group>(
    `${base}/${kind}?${new URLSearchParams({ limit: "50", q: query })}`,
    kind,
    "after_id",
    !id && !creating,
  );
  function navigate(values: Record<string, string>) {
    setParams(values);
  }
  const tabs: [string, string][] =
    kind === "roles"
      ? [
          ["info", "Informacje"],
          ["permissions", "Uprawnienia"],
          ["assignments", "Przypisania"],
        ]
      : [
          ["info", "Informacje"],
          ["members", "Członkowie"],
          ["assignments", "Przypisania"],
        ];
  const tab = tabs.some(([key]) => key === params.get("tab"))
    ? params.get("tab")!
    : "info";
  return (
    <section className="page">
      <div className="page-heading">
        <div>
          <p className="eyebrow">Administracja</p>
          <h1>Role i grupy</h1>
          <p className="muted">
            Zarządzaj uprawnieniami i członkostwem w organizacji.
          </p>
        </div>
        {!id && !creating ? (
          <button
            className="button button--primary"
            onClick={() => navigate({ kind, q: query, create: "1" })}
          >
            {kind === "roles" ? "Nowa rola" : "Nowa grupa"}
          </button>
        ) : null}
      </div>
      <nav className="tabs" aria-label="Listy administracyjne">
        <Link
          className={kind === "roles" ? "is-active" : ""}
          to="/rbac?kind=roles"
          aria-current={kind === "roles" ? "page" : undefined}
        >
          Role
        </Link>
        <Link
          className={kind === "groups" ? "is-active" : ""}
          to="/rbac?kind=groups"
          aria-current={kind === "groups" ? "page" : undefined}
        >
          Grupy
        </Link>
      </nav>
      {id || creating ? (
        <Link to={`/rbac?${new URLSearchParams({ kind, q: query })}`}>
          ← {kind === "roles" ? "Lista ról" : "Lista grup"}
        </Link>
      ) : null}
      {creating ? (
        <CreateResource
          key={kind}
          kind={kind}
          onCreated={(newID) => {
            page.reload();
            navigate({ kind, id: newID, tab: "info", q: query });
          }}
        />
      ) : id ? (
        <>
          <nav className="tabs" aria-label="Szczegóły obiektu">
            {tabs.map(([key, title]) => (
              <Link
                key={key}
                className={tab === key ? "is-active" : ""}
                aria-current={tab === key ? "page" : undefined}
                to={`/rbac?${new URLSearchParams({ kind, id, q: query, tab: key })}`}
              >
                {title}
              </Link>
            ))}
          </nav>
          <ResourceDetail
            key={`${kind}:${id}`}
            kind={kind}
            id={id}
            tab={tab}
            select={(choice) => {
              if (choice) navigate({ kind, id: choice.id, tab, q: query });
            }}
          />
        </>
      ) : (
        <div className="panel">
          <div className="panel-heading">
            <h2>{kind === "roles" ? "Role" : "Grupy"}</h2>
            <button
              className="button button--secondary"
              disabled={page.loading}
              onClick={page.reload}
            >
              Odśwież
            </button>
          </div>
          <form
            className="search-row"
            onSubmit={(event) => {
              event.preventDefault();
              navigate({ kind, q: search.trim() });
            }}
          >
            <label>
              Szukaj {kind === "roles" ? "roli" : "grupy"}
              <input
                value={search}
                onChange={(event) => setSearch(event.target.value)}
                maxLength={200}
              />
            </label>
            <button className="button button--secondary">Szukaj</button>
            {query ? (
              <button
                type="button"
                className="button button--secondary"
                onClick={() => navigate({ kind })}
              >
                Wyczyść
              </button>
            ) : null}
          </form>
          {page.error ? (
            <div className="inline-error" role="alert">
              {page.error}
            </div>
          ) : null}
          {page.loading ? <p role="status">Ładowanie…</p> : null}
          <div
            className="table-wrap"
            tabIndex={0}
            aria-label={kind === "roles" ? "Tabela ról" : "Tabela grup"}
          >
            <table>
              <thead>
                <tr>
                  <th>Nazwa</th>
                  <th>Opis</th>
                  <th>{kind === "roles" ? "Typ" : "Członkowie"}</th>
                </tr>
              </thead>
              <tbody>
                {page.items.map((item) => (
                  <tr key={item.id}>
                    <td>
                      <Link
                        to={`/rbac?${new URLSearchParams({ kind, id: item.id, q: query, tab: "info" })}`}
                      >
                        {item.name}
                      </Link>
                    </td>
                    <td className="wrap-cell">
                      {item.description || "Bez opisu"}
                    </td>
                    <td>
                      {"system_role" in item
                        ? item.system_role
                          ? "Systemowa"
                          : "Własna"
                        : item.member_count}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          {!page.loading && !page.error && !page.items.length ? (
            <div className="empty-cell">
              Brak wyników dla podanych kryteriów.
            </div>
          ) : null}
          {page.next ? (
            <div className="panel-footer">
              <button
                className="button button--secondary"
                disabled={page.loading}
                onClick={() => void page.more()}
              >
                Załaduj więcej
              </button>
            </div>
          ) : null}
        </div>
      )}
    </section>
  );
}

function PermissionFields({
  catalog,
  values,
  onChange,
  disabled,
}: {
  catalog: Permission[];
  values: string[];
  onChange: (values: string[]) => void;
  disabled?: boolean;
}) {
  return (
    <fieldset className="permission-grid" disabled={disabled}>
      <legend>Uprawnienia</legend>
      {catalog.map((permission) => (
        <label className="checkbox-row" key={permission.key}>
          <input
            type="checkbox"
            checked={values.includes(permission.key)}
            onChange={() =>
              onChange(
                values.includes(permission.key)
                  ? values.filter((key) => key !== permission.key)
                  : [...values, permission.key],
              )
            }
          />
          <span>
            <strong>{permissionLabel(permission.key)}</strong>
            <small>{permission.key}</small>
          </span>
        </label>
      ))}
    </fieldset>
  );
}
function useCatalog(enabled = true) {
  const page = useRemotePage<Permission>(
    `${base}/permissions`,
    "permissions",
    "after_id",
    enabled,
  );
  return { catalog: page.items, error: page.error, retry: page.retry };
}
function CreateResource({
  kind,
  onCreated,
}: {
  kind: "roles" | "groups";
  onCreated: (id: string) => void;
}) {
  const {
    catalog,
    error: catalogError,
    retry: retryCatalog,
  } = useCatalog(kind === "roles");
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [permissions, setPermissions] = useState<string[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  async function submit(event: FormEvent) {
    event.preventDefault();
    if (busy) return;
    setBusy(true);
    setError(null);
    try {
      const result = await api<CreatedID>(`${base}/${kind}`, {
        method: "POST",
        body: jsonBody({
          name,
          description,
          ...(kind === "roles" ? { permissions } : {}),
        }),
      });
      onCreated(result.id);
    } catch (cause) {
      setError(
        cause instanceof ApiError && cause.status === 409
          ? "Ta nazwa już istnieje. Wybierz inną nazwę; wpisane dane zostały zachowane."
          : cause instanceof Error
            ? cause.message
            : "Utworzenie nie powiodło się.",
      );
    } finally {
      setBusy(false);
    }
  }
  return (
    <div className="panel">
      <h2>{kind === "roles" ? "Nowa rola" : "Nowa grupa"}</h2>
      <form
        className="form-stack form-stack--compact"
        onSubmit={(event) => void submit(event)}
      >
        <fieldset className="form-fields" disabled={busy}>
          <label>
            Nazwa
            <input
              autoFocus
              value={name}
              onChange={(event) => setName(event.target.value)}
              maxLength={150}
              required
            />
          </label>
          <label>
            Opis
            <textarea
              value={description}
              onChange={(event) => setDescription(event.target.value)}
              maxLength={500}
              rows={3}
            />
          </label>
          {kind === "roles" ? (
            <PermissionFields
              catalog={catalog}
              values={permissions}
              onChange={setPermissions}
            />
          ) : null}
        </fieldset>
        {error || catalogError ? (
          <div role="alert" className="inline-error">
            {error || catalogError}
            {catalogError ? (
              <button
                type="button"
                className="button button--secondary"
                onClick={() => void retryCatalog()}
              >
                Pobierz katalog uprawnień
              </button>
            ) : null}
          </div>
        ) : null}
        <button
          className="button button--primary"
          disabled={
            busy ||
            !name.trim() ||
            (kind === "roles" && (!catalog.length || !!catalogError))
          }
        >
          {busy
            ? "Tworzenie…"
            : kind === "roles"
              ? "Utwórz rolę"
              : "Utwórz grupę"}
        </button>
      </form>
    </div>
  );
}

function ResourceDetail({
  kind,
  id,
  tab,
  select,
}: {
  kind: "roles" | "groups";
  id: string;
  tab: string;
  select: (choice: Choice | null) => void;
}) {
  const [resource, setResource] = useState<Role | Group | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [retry, setRetry] = useState(0);
  const { hasPermission } = useAuth();
  useEffect(() => {
    const controller = new AbortController();
    void api<{ role?: Role; group?: Group }>(
      `${base}/${kind}/${encodeURIComponent(id)}`,
      { signal: controller.signal },
    )
      .then((result) => {
        if (!controller.signal.aborted) {
          setResource(result.role ?? result.group!);
          setError(null);
        }
      })
      .catch((cause: unknown) => {
        if (!controller.signal.aborted)
          setError(
            cause instanceof Error
              ? cause.message
              : "Nie udało się pobrać szczegółów.",
          );
      });
    return () => controller.abort();
  }, [kind, id, retry]);
  if (!resource)
    return (
      <>
        <PageState
          title={
            error ? "Nie można wyświetlić obiektu" : "Ładowanie szczegółów"
          }
          detail={error ?? undefined}
          tone={error ? "error" : undefined}
        />
        {error ? (
          <button
            className="button button--secondary"
            onClick={() => setRetry((value) => value + 1)}
          >
            Spróbuj ponownie
          </button>
        ) : null}
      </>
    );
  return (
    <>
      <div className="panel">
        <h2>{resource.name}</h2>
        {error ? (
          <div role="alert" className="inline-error">
            {error}{" "}
            <button
              className="button button--secondary"
              onClick={() => setRetry((value) => value + 1)}
            >
              Odśwież informacje
            </button>
          </div>
        ) : null}
        <RemoteCombobox
          kind={kind}
          clearable={false}
          label={kind === "roles" ? "Przejdź do roli" : "Przejdź do grupy"}
          value={{ id, label: resource.name }}
          onChange={select}
        />
      </div>
      <div hidden={tab !== "info"} className="panel">
        <h2>Informacje</h2>
        <dl className="detail-grid">
          <div>
            <dt>Nazwa</dt>
            <dd>{resource.name}</dd>
          </div>
          <div>
            <dt>Opis</dt>
            <dd>{resource.description || "Bez opisu"}</dd>
          </div>
          <div>
            <dt>Wersja</dt>
            <dd>{resource.version}</dd>
          </div>
          <div>
            <dt>{"system_role" in resource ? "Typ" : "Członkowie"}</dt>
            <dd>
              {"system_role" in resource
                ? resource.system_role
                  ? "Systemowa"
                  : "Własna"
                : resource.member_count}
            </dd>
          </div>
        </dl>
      </div>
      {"system_role" in resource ? (
        <>
          <div hidden={tab !== "permissions"}>
            <RoleEditor role={resource} onSaved={setResource} />
          </div>
          <div hidden={tab !== "assignments"} className="panel">
            <h2>Przypisania roli</h2>
            <RoleTargets role={resource} />
          </div>
        </>
      ) : (
        <>
          <div hidden={tab !== "members"} className="panel">
            <h2>Członkowie grupy</h2>
            {hasPermission("admin.users.manage") ? (
              <GroupMembers
                groupID={id}
                onChanged={() => setRetry((value) => value + 1)}
              />
            ) : (
              <PageState
                title="Brak uprawnień"
                detail="Lista członków wymaga zarządzania użytkownikami."
              />
            )}
          </div>
          <div hidden={tab !== "assignments"} className="panel">
            <h2>Role grupy</h2>
            <RoleAssignments kind="groups" targetID={id} />
          </div>
        </>
      )}
    </>
  );
}

function RoleEditor({
  role,
  onSaved,
}: {
  role: Role;
  onSaved: (role: Role) => void;
}) {
  const { refresh } = useAuth();
  const { catalog, error: catalogError, retry: retryCatalog } = useCatalog();
  const [version, setVersion] = useState(role.version);
  const [values, setValues] = useState(
    role.permissions.map((permission) => permission.key),
  );
  const [conflict, setConflict] = useState<Role | null>(null);
  const [unresolved, setUnresolved] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [message, setMessage] = useState<string | null>(null);
  async function readConflict() {
    setBusy(true);
    try {
      const result = await api<{ role: Role }>(`${base}/roles/${role.id}`);
      setConflict(result.role);
      setError(null);
    } catch (cause) {
      setError(
        cause instanceof Error
          ? cause.message
          : "Nie udało się pobrać aktualnego stanu.",
      );
    } finally {
      setBusy(false);
    }
  }
  async function submit(event: FormEvent) {
    event.preventDefault();
    if (busy || unresolved || role.system_role) return;
    setBusy(true);
    setError(null);
    setMessage(null);
    try {
      await api<void>(`${base}/roles/${role.id}/permissions`, {
        method: "PUT",
        body: jsonBody({ version, permissions: values }),
      });
      try {
        const result = await api<{ role: Role }>(`${base}/roles/${role.id}`);
        onSaved(result.role);
        setVersion(result.role.version);
        setValues(result.role.permissions.map((permission) => permission.key));
        setMessage("Uprawnienia roli zostały zaktualizowane.");
      } finally {
        // The successful write may have removed this user's permission to read it.
        await refresh();
      }
    } catch (cause) {
      if (cause instanceof ApiError && cause.status === 409) {
        setUnresolved(true);
        await readConflict();
      } else
        setError(
          cause instanceof Error
            ? cause.message
            : "Aktualizacja nie powiodła się.",
        );
    } finally {
      setBusy(false);
    }
  }
  function resolve(useCurrent: boolean) {
    if (!conflict) return;
    setVersion(conflict.version);
    onSaved(conflict);
    if (useCurrent)
      setValues(conflict.permissions.map((permission) => permission.key));
    setConflict(null);
    setUnresolved(false);
  }
  return (
    <div className="panel">
      <h2>Uprawnienia roli</h2>
      {role.system_role ? (
        <p className="muted">
          Uprawnienia roli systemowej są chronione przed edycją.
        </p>
      ) : null}
      <form className="form-stack" onSubmit={(event) => void submit(event)}>
        <PermissionFields
          catalog={catalog}
          values={values}
          onChange={setValues}
          disabled={busy || role.system_role}
        />
        {error || catalogError ? (
          <div role="alert" className="inline-error">
            {error || catalogError}
            {catalogError ? (
              <button
                type="button"
                className="button button--secondary"
                onClick={() => void retryCatalog()}
              >
                Pobierz katalog uprawnień
              </button>
            ) : null}
          </div>
        ) : null}
        {message ? (
          <div role="status" className="inline-success">
            {message}
          </div>
        ) : null}
        {conflict ? (
          <ConflictNotice
            current={
              <PermissionNames
                values={conflict.permissions.map(
                  (permission) => permission.key,
                )}
              />
            }
            proposal={<PermissionNames values={values} />}
            acceptCurrent={() => resolve(true)}
            keepProposal={() => resolve(false)}
          />
        ) : unresolved ? (
          <div role="alert">
            Propozycja została zachowana. Pobierz aktualny stan przed
            rozstrzygnięciem konfliktu.{" "}
            <button
              type="button"
              className="button button--secondary"
              disabled={busy}
              onClick={() => void readConflict()}
            >
              Pobierz aktualny stan
            </button>
          </div>
        ) : null}
        {!role.system_role ? (
          <button
            className="button button--primary"
            disabled={busy || unresolved || !!catalogError || !catalog.length}
          >
            {busy ? "Zapisywanie…" : "Zapisz uprawnienia"}
          </button>
        ) : null}
      </form>
    </div>
  );
}
function PermissionNames({ values }: { values: string[] }) {
  return (
    <ul>
      {values.length ? (
        values.map((value) => <li key={value}>{value}</li>)
      ) : (
        <li>Brak uprawnień</li>
      )}
    </ul>
  );
}

function RoleTargets({ role }: { role: Role }) {
  const { hasPermission } = useAuth();
  const [user, setUser] = useState<Choice | null>(null);
  const [group, setGroup] = useState<Choice | null>(null);
  const defaultRole = { id: role.id, label: role.name };
  return (
    <div className="two-column">
      <section>
        <h3>Role użytkownika</h3>
        {hasPermission("admin.users.manage") ? (
          <>
            <RemoteCombobox
              kind="users"
              label="Użytkownik"
              value={user}
              onChange={setUser}
            />
            {user ? (
              <RoleAssignments
                key={user.id}
                kind="users"
                targetID={user.id}
                initialRole={defaultRole}
              />
            ) : (
              <p className="muted">
                Wybierz użytkownika, aby sprawdzić i zmienić przypisania.
              </p>
            )}
          </>
        ) : (
          <PageState
            title="Brak uprawnień"
            detail="Przypisania użytkowników wymagają zarządzania użytkownikami."
          />
        )}
      </section>
      <section>
        <h3>Role wybranej grupy</h3>
        <RemoteCombobox
          kind="groups"
          label="Grupa"
          value={group}
          onChange={setGroup}
        />
        {group ? (
          <RoleAssignments
            key={group.id}
            kind="groups"
            targetID={group.id}
            initialRole={defaultRole}
          />
        ) : (
          <p className="muted">
            Wybierz grupę, aby sprawdzić i zmienić przypisania.
          </p>
        )}
      </section>
    </div>
  );
}
