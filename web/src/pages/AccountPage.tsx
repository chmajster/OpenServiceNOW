import { useEffect, useRef, useState, type FormEvent } from "react";
import { ApiError, api, jsonBody } from "../api/client";
import type { AuthMe } from "../api/types";
import { useAuth } from "../auth/AuthContext";
import { ConflictNotice } from "../components/ConflictNotice";
import { passwordProblem } from "../auth/passwordPolicy";

type Session = { id: string; current: boolean; tenant_id: string; tenant_slug: string; tenant_name: string; created_at: string; expires_at: string; ip_address: string; user_agent: string };
type SessionPage = { sessions: Session[]; next_after_id?: string };

export function AccountPage() {
  const { session, refresh, changePassword, logout } = useAuth();
  const [name, setName] = useState(session?.user.display_name ?? "");
  const [version, setVersion] = useState(session?.user.version ?? "");
  const [currentPassword, setCurrentPassword] = useState("");
  const [newPassword, setNewPassword] = useState("");
  const [confirmation, setConfirmation] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [conflict, setConflict] = useState<AuthMe | null>(null);
  const [unresolved, setUnresolved] = useState(false);
  const [passwordConflict, setPasswordConflict] = useState<AuthMe | null>(null);
  const [sessions, setSessions] = useState<Session[]>([]);
  const [cursor, setCursor] = useState<string>();
  const [sessionError, setSessionError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const request = useRef<AbortController | null>(null);
  const userID = session?.user.id;
  const required = session?.password_change_required;

  async function loadSessions(after?: string) {
    request.current?.abort();
    const controller = new AbortController(); request.current = controller;
    setLoading(true); setSessionError(null);
    try {
      const page = await api<SessionPage>(`/api/v1/auth/sessions?limit=20${after ? `&after_id=${after}` : ""}`, { signal: controller.signal });
      if (controller.signal.aborted) return;
      setSessions(previous => after ? [...previous, ...page.sessions] : page.sessions);
      setCursor(page.next_after_id);
    } catch (cause) {
      if (!controller.signal.aborted && !(cause instanceof DOMException && cause.name === "AbortError")) setSessionError(cause instanceof Error ? cause.message : "Nie udało się pobrać sesji.");
    } finally { if (!controller.signal.aborted) setLoading(false); }
  }
  useEffect(() => {
    if (!required) void loadSessions();
    return () => request.current?.abort();
  }, [userID, required]);

  async function saveProfile(event: FormEvent) {
    event.preventDefault(); setBusy(true); setError(null); setNotice(null);
    try {
      await api<void>("/api/v1/auth/profile", { method: "PUT", body: jsonBody({ display_name: name, version }) });
      const current = await api<AuthMe>("/api/v1/auth/me");
      setVersion(current.user.version); setName(current.user.display_name); setConflict(null); setUnresolved(false);
      await refresh(); setNotice("Nazwa wyświetlana została zapisana.");
    } catch (cause) {
      if (cause instanceof ApiError && cause.status === 409) {
        setUnresolved(true);
        try { setConflict(await api<AuthMe>("/api/v1/auth/me")); } catch (readError) { setError(readError instanceof Error ? readError.message : "Nie udało się odczytać aktualnego stanu."); }
      } else setError(cause instanceof Error ? cause.message : "Zapis nie powiódł się.");
    } finally { setBusy(false); }
  }

  async function savePassword(event: FormEvent) {
    event.preventDefault(); setError(null); setNotice(null);
    const problem = passwordProblem(newPassword);
    if (problem || newPassword !== confirmation || newPassword === currentPassword) { setError(problem ?? (newPassword !== confirmation ? "Potwierdzenie hasła nie pasuje." : "Nowe hasło musi być inne niż dotychczasowe.")); return; }
    if (!session) return;
    setBusy(true); request.current?.abort();
    try {
      await changePassword({ current_password: currentPassword, new_password: newPassword, credential_version: session.user.credential_version });
      setCurrentPassword(""); setNewPassword(""); setConfirmation("");
      const current = await api<AuthMe>("/api/v1/auth/me");
      if (name === session.user.display_name) { setVersion(current.user.version); setName(current.user.display_name); }
      setNotice("Hasło zmienione. Pozostałe sesje zostały unieważnione, bieżąca sesja została odnowiona.");
      await loadSessions();
    } catch (cause) {
      if (cause instanceof ApiError && cause.status === 409) {
        try { setPasswordConflict(await api<AuthMe>("/api/v1/auth/me")); } catch { setError("Nie udało się odczytać aktualnych poświadczeń. Wpisane dane zachowano. Odśwież stan konta przed ponowieniem."); }
      } else if (cause instanceof ApiError) setError(cause.message);
      else setError("Nie potwierdzono wyniku zmiany hasła. Nie ponawiaj jej automatycznie. Odśwież stan konta; jeśli sesja wygasła, spróbuj zalogować się nowym hasłem.");
    } finally { setBusy(false); }
  }

  async function revoke(id?: string) {
    setBusy(true); setError(null); setNotice(null);
    try {
      await api<void>(`/api/v1/auth/sessions${id ? `/${id}` : ""}`, { method: "DELETE" });
      setNotice(id ? "Sesja została unieważniona." : "Pozostałe sesje zostały unieważnione.");
      await loadSessions();
    } catch (cause) { setError(cause instanceof Error ? cause.message : "Nie udało się unieważnić sesji."); }
    finally { setBusy(false); }
  }

  if (!session) return null;
  return <div className="form-stack">
    <div className="page-heading"><div><h1>Moje konto</h1><p className="muted">Tożsamość konta jest wspólna dla wszystkich organizacji. Adres logowania: {session.user.email}.</p></div></div>
    {required && <div className="notice" role="alert"><strong>Wymagana zmiana hasła</strong><p>Zmień hasło tymczasowe przed korzystaniem z aplikacji. Do tego czasu możesz zmienić hasło lub się wylogować.</p></div>}
    {error && <div className="notice notice--error" role="alert">{error}</div>}
    {notice && <div className="notice" role="status">{notice}</div>}
    {!required && <section className="panel"><h2>Profil</h2><form className="form-stack form-stack--compact" onSubmit={event => void saveProfile(event)}>
      <label>Nazwa wyświetlana<input value={name} onChange={event => setName(event.target.value)} required maxLength={400} disabled={busy} autoComplete="name" /></label>
      <button className="button button--primary" disabled={busy || unresolved || Array.from(name.trim()).length < 1 || Array.from(name.trim()).length > 200}>Zapisz profil</button>
    </form></section>}
    {unresolved && !conflict && <button className="button button--secondary" disabled={busy} onClick={() => void api<AuthMe>("/api/v1/auth/me").then(setConflict).catch(cause => setError(cause instanceof Error ? cause.message : "Odczyt nie powiódł się."))}>Odczytaj aktualny profil</button>}
    {conflict && <ConflictNotice current={<p>{conflict.user.display_name}</p>} proposal={<p>{name}</p>} acceptCurrent={() => { setName(conflict.user.display_name); setVersion(conflict.user.version); setConflict(null); setUnresolved(false); }} keepProposal={() => { setVersion(conflict.user.version); setConflict(null); setUnresolved(false); }} />}
    {passwordConflict && <ConflictNotice current={<p>Wersja poświadczeń: {passwordConflict.user.credential_version}. Hasło nie jest ujawniane.</p>} proposal={<p>Wpisana propozycja hasła została zachowana w formularzu ({Array.from(newPassword).length} znaków). Sprawdź dotychczasowe hasło przed ponownym zapisem.</p>} acceptCurrent={() => { setCurrentPassword(""); setNewPassword(""); setConfirmation(""); setPasswordConflict(null); void refresh().catch(cause => setError(cause instanceof Error ? cause.message : "Odczyt nie powiódł się.")); }} keepProposal={() => { setPasswordConflict(null); void refresh().catch(cause => setError(cause instanceof Error ? cause.message : "Odczyt nie powiódł się.")); }} />}
    <section className="panel"><h2>Zmiana hasła</h2><p className="muted">Co najmniej 12 znaków Unicode i najwyżej 1024 bajty UTF-8. Zmiana wyloguje wszystkie pozostałe sesje we wszystkich organizacjach.</p>
      <form className="form-stack form-stack--compact" onSubmit={event => void savePassword(event)}>
        <label>Dotychczasowe hasło<input type="password" autoComplete="current-password" value={currentPassword} onChange={event => setCurrentPassword(event.target.value)} disabled={busy} required /></label>
        <label>Nowe hasło<input type="password" autoComplete="new-password" value={newPassword} onChange={event => setNewPassword(event.target.value)} disabled={busy} required aria-describedby="password-policy" /></label>
        <span id="password-policy" className="muted">Długość liczona w znakach Unicode, bez zmiany ich zapisu.</span>
        <label>Potwierdź nowe hasło<input type="password" autoComplete="new-password" value={confirmation} onChange={event => setConfirmation(event.target.value)} disabled={busy} required /></label>
        <div className="action-row"><button className="button button--primary" disabled={busy || !!passwordConflict}>Zmień hasło</button><button type="button" className="button button--secondary" disabled={busy} onClick={() => void refresh().catch(cause => setError(cause instanceof Error ? cause.message : "Odświeżenie nie powiodło się."))}>Odśwież stan konta</button></div>
      </form>
    </section>
    {!required && <section className="panel"><div className="page-heading"><h2>Aktywne sesje</h2><button className="button button--secondary" disabled={busy || loading} onClick={() => void revoke()}>Unieważnij pozostałe sesje</button></div>
      {sessionError && <div className="notice notice--error" role="alert">{sessionError}</div>}
      {loading && <p role="status">Ładowanie sesji…</p>}
      {!loading && !sessionError && sessions.length === 0 && <p>Brak aktywnych sesji.</p>}
      <div className="table-scroll"><table><thead><tr><th>Sesja</th><th>Organizacja</th><th>Utworzona</th><th>Wygasa</th><th>Urządzenie</th><th>Akcje</th></tr></thead><tbody>{sessions.map(item => <tr key={item.id}>
        <td>{item.current ? "Bieżąca sesja" : `#${item.id}`}</td><td>{item.tenant_name}<small className="muted"> ({item.tenant_slug})</small></td><td>{new Date(item.created_at).toLocaleString("pl-PL")}</td><td>{new Date(item.expires_at).toLocaleString("pl-PL")}</td><td><span>{item.ip_address || "Brak adresu"}</span><details><summary>Przeglądarka</summary>{item.user_agent || "Brak danych"}</details></td><td><button className="button button--small button--secondary" disabled={busy || loading} onClick={() => { if (item.current) void logout().catch(cause => setError(cause instanceof Error ? cause.message : "Wylogowanie nie powiodło się.")); else void revoke(item.id); }}>{item.current ? "Wyloguj" : "Unieważnij"}</button></td>
      </tr>)}</tbody></table></div>
      <div className="action-row"><button className="button button--secondary" disabled={busy || loading} onClick={() => void loadSessions()}>Odśwież sesje</button>{cursor && <button className="button button--secondary" disabled={busy || loading} onClick={() => void loadSessions(cursor)}>Wczytaj kolejne sesje</button>}</div>
    </section>}
  </div>;
}
