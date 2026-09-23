import { useState, type FormEvent } from "react";
import { Navigate, useNavigate } from "react-router-dom";
import { ApiError } from "../api/client";
import { useAuth } from "../auth/AuthContext";

export function LoginPage() {
  const { status, login } = useAuth();
  const navigate = useNavigate();
  const [tenant, setTenant] = useState("default");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  if (status === "authenticated") {
    return <Navigate to="/" replace />;
  }

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setSubmitting(true);
    setError(null);
    try {
      await login({ tenant, email, password });
      navigate("/", { replace: true });
    } catch (cause) {
      if (cause instanceof ApiError && cause.status === 429) {
        setError("Zbyt wiele prób logowania. Spróbuj ponownie później.");
      } else if (cause instanceof ApiError && cause.status === 401) {
        setError("Nieprawidłowy tenant, e-mail lub hasło.");
      } else {
        setError(cause instanceof Error ? cause.message : "Logowanie nie powiodło się.");
      }
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <main className="login-layout">
      <section className="login-panel">
        <div className="brand brand--login">
          <span className="brand-mark">OS</span>
          <div>
            <strong>OpenServiceNOW</strong>
            <small>IT Service Platform</small>
          </div>
        </div>

        <div>
          <p className="eyebrow">Dostęp do platformy</p>
          <h1>Zaloguj się</h1>
          <p className="muted">Podaj tenant oraz dane lokalnego konta.</p>
        </div>

        <form className="form-stack" onSubmit={(event) => void handleSubmit(event)}>
          <label>
            Tenant
            <input
              value={tenant}
              onChange={(event) => setTenant(event.target.value)}
              autoComplete="organization"
              required
            />
          </label>
          <label>
            E-mail
            <input
              type="email"
              value={email}
              onChange={(event) => setEmail(event.target.value)}
              autoComplete="username"
              required
            />
          </label>
          <label>
            Hasło
            <input
              type="password"
              value={password}
              onChange={(event) => setPassword(event.target.value)}
              autoComplete="current-password"
              minLength={12}
              maxLength={1024}
              required
            />
          </label>
          {error ? <div className="inline-error" role="alert">{error}</div> : null}
          <button className="button button--primary" type="submit" disabled={submitting}>
            {submitting ? "Logowanie…" : "Zaloguj"}
          </button>
        </form>
      </section>
      <section className="login-aside" aria-hidden="true">
        <div>
          <span className="eyebrow">Foundation</span>
          <h2>Jedno miejsce dla operacji IT.</h2>
          <p>Incydenty, zgłoszenia, CMDB, automatyzacja i audyt będą rozwijane na wspólnym fundamencie uprawnień i tenantów.</p>
        </div>
      </section>
    </main>
  );
}
