import { useEffect, useState, type FormEvent } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { ApiError, api, jsonBody } from "../api/client";
import type { CreatedID, TenantUser } from "../api/types";
import { useAuth } from "../auth/AuthContext";
import { useRemotePage } from "../components/useRemotePage";
import { passwordProblem } from "../auth/passwordPolicy";

export function UsersPage() {
  const { session, refresh } = useAuth();
  const [params, setParams] = useSearchParams();
  const query = params.get("q") ?? "";
  const mode = params.get("form") ?? "";
  const [search, setSearch] = useState(query);
  useEffect(() => setSearch(query), [query]);
  const page = useRemotePage<TenantUser>(
    `/api/v1/admin/users/?${new URLSearchParams({ limit: "50", q: query })}`,
    "users",
  );
  const [email, setEmail] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [password, setPassword] = useState("");
  const [existingEmail, setExistingEmail] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [message, setMessage] = useState<string | null>(null);
  function navigate(form: string) {
    setParams({ q: query, ...(form ? { form } : {}) });
    setError(null);
    setMessage(null);
  }
  async function create(event: FormEvent, existing: boolean) {
    event.preventDefault();
    if (busy) return;
    if (!existing && passwordProblem(password)) { setError(passwordProblem(password)); return; }
    setBusy(true);
    setError(null);
    setMessage(null);
    try {
      await api<CreatedID>(
        existing ? "/api/v1/admin/users/memberships" : "/api/v1/admin/users/",
        {
          method: "POST",
          body: jsonBody(
            existing
              ? { email: existingEmail }
              : { email, display_name: displayName, password },
          ),
        },
      );
      if (existing) setExistingEmail("");
      else {
        setEmail("");
        setDisplayName("");
        setPassword("");
      }
      setParams({ q: existing ? existingEmail : email });
      page.reload();
      setMessage(
        existing
          ? "Konto zostało dodane do organizacji."
          : "Użytkownik został utworzony.",
      );
    } catch (cause) {
      setError(
        cause instanceof ApiError && cause.status === 409
          ? existing
            ? "Konto ma już członkostwo w tej organizacji. Użyj akcji Włącz, jeśli jest nieaktywne."
            : "Konto z tym adresem e-mail już istnieje. Użyj akcji Dodaj istniejące konto."
          : cause instanceof ApiError && cause.status === 404
            ? "Nie znaleziono aktywnego konta z tym adresem e-mail."
            : cause instanceof Error
              ? cause.message
              : "Nie udało się zapisać danych.",
      );
    } finally {
      setBusy(false);
    }
  }
  async function membership(user: TenantUser) {
    if (busy || page.loading || page.error) return;
    setBusy(true);
    setError(null);
    setMessage(null);
    try {
      await api<void>(`/api/v1/admin/users/${user.id}/membership`, {
        method: "PUT",
        body: jsonBody({ active: !user.membership_active }),
      });
      page.reload();
      setMessage(
        user.membership_active
          ? "Członkostwo zostało wyłączone."
          : "Członkostwo zostało włączone.",
      );
      await refresh();
    } catch (cause) {
      setError(
        cause instanceof Error
          ? cause.message
          : "Zmiana członkostwa nie powiodła się.",
      );
    } finally {
      setBusy(false);
    }
  }
  return (
    <section className="page">
      <div className="page-heading">
        <div>
          <p className="eyebrow">Administracja</p>
          <h1>Użytkownicy</h1>
          <p className="muted">Konta i członkostwo w bieżącej organizacji.</p>
        </div>
        <div className="action-row">
          <button
            className="button button--primary"
            disabled={busy}
            onClick={() => navigate("create")}
          >
            Nowy użytkownik
          </button>
          <button
            className="button button--secondary"
            disabled={busy}
            onClick={() => navigate("existing")}
          >
            Dodaj istniejące konto
          </button>
        </div>
      </div>
      {mode === "create" || mode === "existing" ? (
        <div className="panel">
          <h2>
            {mode === "create"
              ? "Dodaj konto lokalne"
              : "Dodaj istniejące konto"}
          </h2>
          <form
            className="form-stack"
            onSubmit={(event) => void create(event, mode === "existing")}
          >
            <fieldset className="form-fields" disabled={busy}>
              {mode === "create" ? (
                <>
                  <label>
                    E-mail
                    <input
                      autoFocus
                      type="email"
                      value={email}
                      onChange={(event) => setEmail(event.target.value)}
                      maxLength={254}
                      required
                    />
                  </label>
                  <label>
                    Nazwa wyświetlana
                    <input
                      value={displayName}
                      onChange={(event) => setDisplayName(event.target.value)}
                      maxLength={200}
                      required
                    />
                  </label>
                  <label>
                    Hasło
                    <input
                      type="password"
                      autoComplete="new-password"
                      value={password}
                      onChange={(event) => setPassword(event.target.value)}
                      maxLength={1024}
                      required
                    />
                  </label>
                  <p className="muted">To hasło tymczasowe. Użytkownik musi je zmienić po pierwszym logowaniu. Co najmniej 12 znaków Unicode, najwyżej 1024 bajty UTF-8.</p>
                </>
              ) : (
                <>
                  <p className="muted">
                    Dodaje członkostwo bez zmiany profilu i hasła istniejącego
                    konta.
                  </p>
                  <label>
                    E-mail istniejącego konta
                    <input
                      autoFocus
                      type="email"
                      value={existingEmail}
                      onChange={(event) => setExistingEmail(event.target.value)}
                      maxLength={254}
                      required
                    />
                  </label>
                </>
              )}
            </fieldset>
            <div className="action-row">
              <button className="button button--primary" disabled={busy}>
                {busy
                  ? "Zapisywanie…"
                  : mode === "create"
                    ? "Dodaj użytkownika"
                    : "Dodaj członkostwo"}
              </button>
              <button
                type="button"
                className="button button--secondary"
                disabled={busy}
                onClick={() => navigate("")}
              >
                Zamknij formularz
              </button>
            </div>
          </form>
        </div>
      ) : null}
      {error || page.error ? (
        <div role="alert" className="inline-error">
          {error || page.error}
        </div>
      ) : null}
      {message ? (
        <div role="status" className="inline-success">
          {message}
        </div>
      ) : null}
      <div className="panel">
        <div className="panel-heading">
          <h2>Konta w organizacji</h2>
          <button
            className="button button--secondary"
            disabled={page.loading || busy}
            onClick={page.reload}
          >
            Odśwież
          </button>
        </div>
        <form
          className="search-row"
          onSubmit={(event) => {
            event.preventDefault();
            setParams({ q: search.trim(), ...(mode ? { form: mode } : {}) });
          }}
        >
          <label>
            Szukaj po nazwie lub e-mailu
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
              onClick={() => setParams(mode ? { form: mode } : {})}
            >
              Wyczyść
            </button>
          ) : null}
        </form>
        {page.loading ? <p role="status">Ładowanie użytkowników…</p> : null}
        <div
          className="table-wrap"
          tabIndex={0}
          aria-label="Tabela użytkowników"
        >
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
              {page.items.map((user) => (
                <tr key={user.id}>
                  <td>
                    <Link to={`/users/${user.id}`}>{user.display_name}</Link>
                  </td>
                  <td>{user.email}</td>
                  <td>{user.user_active ? "Aktywne" : "Wyłączone"}</td>
                  <td>
                    <span
                      className={
                        user.membership_active
                          ? "badge badge--success"
                          : "badge"
                      }
                    >
                      {user.membership_active ? "Aktywne" : "Wyłączone"}
                    </span>
                  </td>
                  <td>
                    <button
                      className="button button--secondary"
                      disabled={
                        busy ||
                        page.loading ||
                        !!page.error ||
                        (user.id === session?.user.id && user.membership_active)
                      }
                      title={
                        user.id === session?.user.id
                          ? "Nie można wyłączyć własnego członkostwa"
                          : undefined
                      }
                      onClick={() => void membership(user)}
                    >
                      {user.membership_active ? "Wyłącz" : "Włącz"}
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        {!page.loading && !page.error && !page.items.length ? (
          <p className="empty-cell">
            Brak użytkowników dla podanych kryteriów.
          </p>
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
    </section>
  );
}
