import { useEffect, useState, type FormEvent } from "react";
import { ApiError, api, jsonBody } from "../api/client";
import type { CreatedID, TenantUser, UsersResponse } from "../api/types";
import { useAuth } from "../auth/AuthContext";
import { PageState } from "../components/PageState";

export function UsersPage() {
  const { session } = useAuth();
  const [users, setUsers] = useState<TenantUser[]>([]);
  const [nextAfterID, setNextAfterID] = useState<string | undefined>();
  const [query, setQuery] = useState("");
  const [appliedQuery, setAppliedQuery] = useState("");
  const [loading, setLoading] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [email, setEmail] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [password, setPassword] = useState("");
  const [creating, setCreating] = useState(false);
  const [changingID, setChangingID] = useState<string | null>(null);

  async function loadUsers(search = appliedQuery, afterID?: string, append = false) {
    append ? setLoadingMore(true) : setLoading(true);
    try {
      const params = new URLSearchParams({ limit: "50" });
      if (search.trim()) {
        params.set("q", search.trim());
      }
      if (afterID) {
        params.set("after_id", afterID);
      }
      const response = await api<UsersResponse>(`/api/v1/admin/users/?${params.toString()}`);
      setUsers((current) => (append ? [...current, ...response.users] : response.users));
      setNextAfterID(response.next_after_id);
      setError(null);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Nie udało się pobrać użytkowników.");
    } finally {
      setLoading(false);
      setLoadingMore(false);
    }
  }

  useEffect(() => {
    void loadUsers("");
  }, []);

  async function handleSearch(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const normalized = query.trim();
    setAppliedQuery(normalized);
    setUsers([]);
    setNextAfterID(undefined);
    await loadUsers(normalized);
  }

  async function handleCreate(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setCreating(true);
    setError(null);
    try {
      await api<CreatedID>("/api/v1/admin/users/", {
        method: "POST",
        body: jsonBody({
          email,
          display_name: displayName,
          password
        })
      });
      setEmail("");
      setDisplayName("");
      setPassword("");
      setAppliedQuery("");
      setQuery("");
      await loadUsers("");
    } catch (cause) {
      if (cause instanceof ApiError && cause.status === 409) {
        setError("Konto z tym adresem e-mail już istnieje.");
      } else {
        setError(cause instanceof Error ? cause.message : "Utworzenie użytkownika nie powiodło się.");
      }
    } finally {
      setCreating(false);
    }
  }

  async function setMembership(user: TenantUser, active: boolean) {
    setChangingID(user.id);
    setError(null);
    try {
      await api<void>(`/api/v1/admin/users/${user.id}/membership`, {
        method: "PUT",
        body: jsonBody({ active })
      });
      await loadUsers(appliedQuery);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Zmiana członkostwa nie powiodła się.");
    } finally {
      setChangingID(null);
    }
  }

  if (loading && users.length === 0 && !appliedQuery) {
    return <PageState title="Ładowanie użytkowników" />;
  }

  return (
    <section className="page">
      <div className="page-heading">
        <div>
          <p className="eyebrow">Administracja</p>
          <h1>Użytkownicy</h1>
          <p className="muted">Lokalne konta i członkostwo w bieżącym tenancie.</p>
        </div>
      </div>

      <div className="panel">
        <div className="panel-heading">
          <div>
            <h2>Dodaj konto lokalne</h2>
            <p className="muted">Hasło jest haszowane Argon2id po stronie backendu.</p>
          </div>
        </div>
        <form className="form-grid" onSubmit={(event) => void handleCreate(event)}>
          <label>
            E-mail
            <input type="email" value={email} onChange={(event) => setEmail(event.target.value)} required />
          </label>
          <label>
            Nazwa wyświetlana
            <input value={displayName} onChange={(event) => setDisplayName(event.target.value)} maxLength={200} required />
          </label>
          <label>
            Hasło
            <input
              type="password"
              value={password}
              onChange={(event) => setPassword(event.target.value)}
              minLength={12}
              maxLength={1024}
              required
            />
          </label>
          <div className="form-action">
            <button className="button button--primary" type="submit" disabled={creating}>
              {creating ? "Tworzenie…" : "Dodaj użytkownika"}
            </button>
          </div>
        </form>
      </div>

      {error ? <div className="inline-error" role="alert">{error}</div> : null}

      <div className="panel">
        <div className="panel-heading">
          <div>
            <h2>Konta w tenancie</h2>
            <p className="muted">
              {users.length} załadowanych rekordów{appliedQuery ? ` dla „${appliedQuery}”` : ""}
            </p>
          </div>
          <button className="button button--secondary" onClick={() => void loadUsers(appliedQuery)} disabled={loading}>
            Odśwież
          </button>
        </div>

        <form className="search-row" onSubmit={(event) => void handleSearch(event)}>
          <label>
            Szukaj po nazwie lub e-mailu
            <input
              value={query}
              onChange={(event) => setQuery(event.target.value)}
              maxLength={200}
              placeholder="np. administrator albo @example.test"
            />
          </label>
          <button className="button button--secondary" type="submit" disabled={loading}>Szukaj</button>
          {appliedQuery ? (
            <button
              className="button button--secondary"
              type="button"
              onClick={() => {
                setQuery("");
                setAppliedQuery("");
                void loadUsers("");
              }}
            >
              Wyczyść
            </button>
          ) : null}
        </form>

        <div className="table-wrap">
          <table>
            <thead>
              <tr>
                <th>Użytkownik</th>
                <th>E-mail</th>
                <th>Konto</th>
                <th>Członkostwo</th>
                <th>Akcja</th>
              </tr>
            </thead>
            <tbody>
              {users.map((user) => {
                const self = user.id === session?.user.id;
                return (
                  <tr key={user.id}>
                    <td>{user.display_name}</td>
                    <td>{user.email}</td>
                    <td>{user.user_active ? "Aktywne" : "Wyłączone"}</td>
                    <td>
                      <span className={user.membership_active ? "badge badge--success" : "badge"}>
                        {user.membership_active ? "Aktywne" : "Wyłączone"}
                      </span>
                    </td>
                    <td>
                      <button
                        className="button button--small button--secondary"
                        disabled={changingID === user.id || (self && user.membership_active)}
                        title={self && user.membership_active ? "Nie można wyłączyć własnego członkostwa" : undefined}
                        onClick={() => void setMembership(user, !user.membership_active)}
                      >
                        {user.membership_active ? "Wyłącz" : "Włącz"}
                      </button>
                    </td>
                  </tr>
                );
              })}
              {users.length === 0 && !loading ? (
                <tr>
                  <td colSpan={5} className="empty-cell">Brak użytkowników dla podanych kryteriów.</td>
                </tr>
              ) : null}
            </tbody>
          </table>
        </div>

        {nextAfterID ? (
          <div className="panel-footer">
            <button
              className="button button--secondary"
              disabled={loadingMore}
              onClick={() => void loadUsers(appliedQuery, nextAfterID, true)}
            >
              {loadingMore ? "Ładowanie…" : "Załaduj więcej"}
            </button>
          </div>
        ) : null}
      </div>
    </section>
  );
}
