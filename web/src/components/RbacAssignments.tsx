import { useRef, useEffect, useState, type FormEvent } from "react";
import { Link } from "react-router-dom";
import { api } from "../api/client";
import type { AssignedRole, GroupMember } from "../api/types";
import { useAuth } from "../auth/AuthContext";
import { RemoteCombobox, type Choice } from "./RemoteCombobox";
import { useRemotePage } from "./useRemotePage";

// These editors are keyed by the target ID by their caller. An operation captures
// that identity; an unmounted editor cannot refetch or report another object's data.
function useMutation(reload: () => void) {
  const { refresh } = useAuth();
  const mounted = useRef(true);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [message, setMessage] = useState<string | null>(null);
  const locked = useRef(false);
  async function mutate(path: string, method: string, success: string) {
    if (locked.current) return false;
    locked.current = true;
    setBusy(true);
    setError(null);
    setMessage(null);
    try {
      await api<void>(path, { method });
      if (mounted.current) {
        reload();
        setMessage(success);
      }
      await refresh();
      return true;
    } catch (cause) {
      if (mounted.current)
        setError(
          cause instanceof Error ? cause.message : "Zmiana nie powiodła się.",
        );
      return false;
    } finally {
      locked.current = false;
      if (mounted.current) setBusy(false);
    }
  }
  return { busy, error, message, mutate };
}
export function RoleAssignments({
  kind,
  targetID,
  initialRole = null,
}: {
  kind: "users" | "groups";
  targetID: string;
  initialRole?: Choice | null;
}) {
  const endpoint = `/api/v1/admin/rbac/${kind}/${targetID}/roles`;
  const page = useRemotePage<AssignedRole>(`${endpoint}?limit=50`, "roles");
  const [role, setRole] = useState<Choice | null>(initialRole);
  const mutation = useMutation(page.reload);
  const blocked = mutation.busy || page.loading || !!page.error;
  async function assign(event: FormEvent) {
    event.preventDefault();
    if (blocked || !role) return;
    if (
      await mutation.mutate(
        `${endpoint}/${role.id}`,
        "POST",
        kind === "users"
          ? "Rola została przypisana użytkownikowi."
          : "Rola została przypisana grupie.",
      )
    )
      setRole(null);
  }
  return (
    <div className="form-stack">
      <form className="form-stack" onSubmit={(event) => void assign(event)}>
        <RemoteCombobox
          kind="roles"
          label="Rola do przypisania"
          value={role}
          onChange={setRole}
          disabled={mutation.busy}
        />
        <button
          className="button button--secondary"
          disabled={blocked || !role}
        >
          Przypisz rolę
        </button>
      </form>
      <Feedback
        loading={page.loading}
        error={page.error || mutation.error}
        message={mutation.message}
        retry={() => void page.retry()}
      />
      <div className="assignment-list" aria-label="Przypisane role">
        {page.items.map((item) => (
          <div className="assignment-row" key={item.id}>
            <span>
              <strong>{item.name}</strong>
              <small>
                {item.system_role ? "Rola systemowa" : "Rola własna"}
              </small>
            </span>
            <button
              className="button button--secondary"
              disabled={blocked}
              onClick={() =>
                void mutation.mutate(
                  `${endpoint}/${item.id}`,
                  "DELETE",
                  "Rola została usunięta.",
                )
              }
              aria-label={`Usuń rolę ${item.name}`}
            >
              Usuń
            </button>
          </div>
        ))}
      </div>
      {!page.loading && !page.error && !page.items.length ? (
        <p className="empty-cell">Brak bezpośrednio przypisanych ról.</p>
      ) : null}
      {page.next ? (
        <button
          className="button button--secondary"
          disabled={blocked}
          onClick={() => void page.more()}
        >
          Załaduj więcej przypisań
        </button>
      ) : null}
    </div>
  );
}
export function GroupMembers({
  groupID,
  onChanged,
}: {
  groupID: string;
  onChanged: () => void;
}) {
  const endpoint = `/api/v1/admin/rbac/groups/${groupID}/members`;
  const page = useRemotePage<GroupMember>(`${endpoint}?limit=50`, "members");
  const [user, setUser] = useState<Choice | null>(null);
  const mutation = useMutation(() => {
    page.reload();
    onChanged();
  });
  const blocked = mutation.busy || page.loading || !!page.error;
  async function add(event: FormEvent) {
    event.preventDefault();
    if (blocked || !user) return;
    if (
      await mutation.mutate(
        `${endpoint}/${user.id}`,
        "POST",
        "Użytkownik został dodany do grupy.",
      )
    )
      setUser(null);
  }
  return (
    <div className="form-stack">
      <form className="form-stack" onSubmit={(event) => void add(event)}>
        <RemoteCombobox
          kind="users"
          label="Użytkownik do dodania"
          value={user}
          onChange={setUser}
          disabled={mutation.busy}
        />
        <button
          className="button button--secondary"
          disabled={blocked || !user}
        >
          Dodaj do grupy
        </button>
      </form>
      <Feedback
        loading={page.loading}
        error={page.error || mutation.error}
        message={mutation.message}
        retry={() => void page.retry()}
      />
      <div className="table-wrap" tabIndex={0} aria-label="Tabela członków">
        <table>
          <thead>
            <tr>
              <th>Użytkownik</th>
              <th>E-mail</th>
              <th>Członkostwo</th>
              <th>Akcja</th>
            </tr>
          </thead>
          <tbody>
            {page.items.map((member) => (
              <tr key={member.user_id}>
                <td>
                  <Link to={`/users/${member.user_id}`}>
                    {member.display_name}
                  </Link>
                </td>
                <td>{member.email}</td>
                <td>
                  {member.user_active && member.membership_active
                    ? "Aktywne"
                    : "Nieaktywne"}
                </td>
                <td>
                  <button
                    className="button button--secondary"
                    disabled={blocked}
                    aria-label={`Usuń członka ${member.display_name}`}
                    onClick={() =>
                      void mutation.mutate(
                        `${endpoint}/${member.user_id}`,
                        "DELETE",
                        "Użytkownik został usunięty z grupy.",
                      )
                    }
                  >
                    Usuń
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {!page.loading && !page.error && !page.items.length ? (
        <p className="empty-cell">Grupa nie ma członków.</p>
      ) : null}
      {page.next ? (
        <button
          className="button button--secondary"
          disabled={blocked}
          onClick={() => void page.more()}
        >
          Załaduj więcej członków
        </button>
      ) : null}
    </div>
  );
}
function Feedback({
  loading,
  error,
  message,
  retry,
}: {
  loading: boolean;
  error: string | null;
  message: string | null;
  retry: () => void;
}) {
  return (
    <>
      {loading ? <p role="status">Ładowanie przypisań…</p> : null}
      {error ? (
        <div className="inline-error" role="alert">
          {error}{" "}
          <button
            type="button"
            className="button button--small button--secondary"
            onClick={retry}
            disabled={loading}
          >
            Odśwież stan
          </button>
        </div>
      ) : null}
      {message ? (
        <div className="inline-success" role="status">
          {message}
        </div>
      ) : null}
    </>
  );
}
