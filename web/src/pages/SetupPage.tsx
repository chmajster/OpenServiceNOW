import { useRef, useState, type FormEvent } from "react";
import { Navigate, useNavigate } from "react-router-dom";
import { ApiError, api, jsonBody } from "../api/client";
import type { SetupResult, SetupStatus } from "../api/types";
import { useAuth } from "../auth/AuthContext";
import { PageState } from "../components/PageState";

export function SetupPage() {
  const { status, setup, error: connectionError, refresh } = useAuth();
  const navigate = useNavigate();
  const [tenant, setTenant] = useState("default");
  const [tenantName, setTenantName] = useState("");
  const [displayName, setDisplayName] = useState("Administrator");
  const [email, setEmail] = useState("admin");
  const [password, setPassword] = useState("");
  const [confirmation, setConfirmation] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [completed, setCompleted] = useState<SetupResult | null>(null);
  const [closed, setClosed] = useState(false);
  const submitLock = useRef(false);

  async function goToLogin() {
    setSubmitting(true);
    setError(null);
    try {
      await refresh();
      navigate("/login", {
        replace: true,
        state: completed ? {
          tenant: completed.tenant,
          email: email.trim().toLowerCase() === "admin" ? "admin" : completed.email,
          notice: "Konto administratora zostało utworzone. Zaloguj się."
        } : { notice: "Konfiguracja jest zakończona. Zaloguj się do istniejącego konta." }
      });
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Nie można połączyć się z aplikacją.");
    } finally {
      setSubmitting(false);
    }
  }

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (submitLock.current) return;
    setError(null);
    if (password !== confirmation) {
      setError("Hasła muszą być takie same.");
      return;
    }
    const identifier = email.trim().toLowerCase();
    const size = new TextEncoder().encode(password).length;
    if (Array.from(password).length < 12 && !(password === "admin" && ["admin", "admin@example.test"].includes(identifier))) {
      setError("Własne hasło musi mieć co najmniej 12 znaków. Dla loginu admin możesz użyć hasła admin.");
      return;
    }
    if (size > 1024) {
      setError("Hasło jest zbyt długie. Użyj krótszego hasła.");
      return;
    }
    if (!setup?.token) {
      setError("Nie można sprawdzić sesji instalatora. Odśwież stronę i spróbuj ponownie.");
      return;
    }
    submitLock.current = true;
    setSubmitting(true);
    try {
      const result = await api<SetupResult>("/api/v1/setup", {
        method: "POST",
        headers: { "X-Setup-Token": setup.token },
        body: jsonBody({ tenant, tenant_name: tenantName, email, display_name: displayName, password })
      });
      setCompleted(result);
      setPassword("");
      setConfirmation("");
    } catch (cause) {
      if (cause instanceof ApiError && cause.code === "SETUP_COMPLETED") {
        setClosed(true);
      } else if (cause instanceof ApiError && cause.code === "SETUP_TOKEN_INVALID") {
        try {
          await refresh();
          setError("Sesja instalatora wygasła. Dane formularza są zachowane; spróbuj ponownie.");
        } catch {
          setError("Nie można odświeżyć sesji instalatora. Sprawdź połączenie i odśwież stronę.");
        }
      } else if (cause instanceof ApiError && cause.status === 429) {
        setError("Zbyt wiele prób. Poczekaj minutę i spróbuj ponownie.");
      } else {
        setError(cause instanceof Error ? cause.message : "Nie udało się utworzyć konta.");
        // A lost response can follow a committed installation. Read the durable
        // status before offering another attempt.
        if (!(cause instanceof ApiError) || cause.status >= 500) {
          try {
            const installation = await api<SetupStatus>("/api/v1/setup");
            if (!installation.required) {
              setClosed(true);
              setError(null);
            }
          } catch { /* The visible error remains until the connection recovers. */ }
        }
      }
    } finally {
      submitLock.current = false;
      setSubmitting(false);
    }
  }

  if (status === "authenticated") return <Navigate to="/" replace />;
  if (!completed && !closed && status === "anonymous") return <Navigate to="/login" replace />;

  return (
    <main className="setup-layout">
      <section className="setup-panel" aria-labelledby="setup-title">
        <div className="brand brand--login">
          <span className="brand-mark">OS</span>
          <div><strong>OpenServiceNOW</strong><small>IT Service Platform</small></div>
        </div>
        {status === "loading" ? (
          <PageState title="Sprawdzanie instalacji" detail="Łączenie z aplikacją." />
        ) : completed || closed ? (
          <div className="form-stack">
            <h1 id="setup-title">Instalacja zakończona</h1>
            <PageState
              title={completed ? "Konto administratora zostało utworzone." : "Ta instalacja jest już skonfigurowana."}
              detail={completed ? `Organizacja: ${completed.tenant}. Możesz teraz zalogować się i rozpocząć pracę.` : "Konfiguracja została zakończona w innym oknie. Użyj istniejącego konta."}
              tone="success"
            />
            {error ? <div className="inline-error" role="alert">{error}</div> : null}
            <button className="button button--primary" type="button" disabled={submitting} onClick={() => void goToLogin()}>
              {submitting ? "Łączenie…" : "Przejdź do logowania"}
            </button>
          </div>
        ) : status === "error" ? (
          <div className="form-stack">
            <PageState title="Nie można sprawdzić instalacji" detail={connectionError ?? "Aplikacja nie odpowiada."} tone="error" />
            <button className="button button--primary" type="button" onClick={() => void refresh().catch(() => undefined)}>Spróbuj ponownie</button>
          </div>
        ) : (
          <>
            <p className="eyebrow">Pierwsze uruchomienie</p>
            <h1 id="setup-title">Utwórz konto administratora</h1>
            <p className="muted">Skonfiguruj organizację i dane pierwszego konta. Ten kreator jest dostępny tylko przed zakończeniem instalacji.</p>
            <form className="form-stack" onSubmit={(event) => void handleSubmit(event)}>
              <div className="setup-fields">
                <fieldset className="setup-section" disabled={submitting}>
                  <legend>Organizacja</legend>
                  <label>Nazwa organizacji<input value={tenantName} onChange={(event) => setTenantName(event.target.value)} autoComplete="organization" maxLength={200} required /></label>
                  <label>Identyfikator organizacji (tenant)<input value={tenant} onChange={(event) => setTenant(event.target.value)} pattern="[a-z0-9](?:[a-z0-9-]{0,98}[a-z0-9])?" maxLength={100} aria-describedby="tenant-help" required /></label>
                  <small id="tenant-help" className="muted">Małe litery, cyfry i myślniki. Będzie używany podczas logowania.</small>
                </fieldset>
                <fieldset className="setup-section" disabled={submitting}>
                  <legend>Administrator</legend>
                  <label>Nazwa administratora<input value={displayName} onChange={(event) => setDisplayName(event.target.value)} autoComplete="name" maxLength={200} required /></label>
                  <label>Login lub e-mail<input value={email} onChange={(event) => setEmail(event.target.value)} autoComplete="username" maxLength={254} aria-describedby="login-help" required /></label>
                  <small id="login-help" className="muted">Użyj loginu admin lub własnego adresu e-mail.</small>
                  <label>Hasło<input type="password" value={password} onChange={(event) => setPassword(event.target.value)} autoComplete="new-password" maxLength={1024} aria-describedby="password-help" required /></label>
                  <small id="password-help" className="muted">Własne hasło: minimum 12 znaków Unicode, najwyżej 1024 bajty UTF-8. Domyślne hasło admin wymaga zmiany po pierwszym logowaniu.</small>
                  <label>Powtórz hasło<input type="password" value={confirmation} onChange={(event) => setConfirmation(event.target.value)} autoComplete="new-password" maxLength={1024} required /></label>
                </fieldset>
              </div>
              {error ? <div className="inline-error" role="alert">{error}</div> : null}
              <button className="button button--primary" type="submit" disabled={submitting}>
                {submitting ? "Tworzenie konta…" : "Utwórz konto i zakończ instalację"}
              </button>
            </form>
          </>
        )}
      </section>
    </main>
  );
}
