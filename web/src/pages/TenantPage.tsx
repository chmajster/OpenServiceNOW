import { useEffect, useState, type FormEvent } from "react";
import { ApiError, api, jsonBody } from "../api/client";
import type { Tenant, TenantResponse } from "../api/types";
import { PageState } from "../components/PageState";

export function TenantPage() {
  const [tenant, setTenant] = useState<Tenant | null>(null);
  const [name, setName] = useState("");
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [message, setMessage] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  async function loadTenant() {
    setLoading(true);
    try {
      const response = await api<TenantResponse>("/api/v1/admin/tenant/");
      setTenant(response.tenant);
      setName(response.tenant.name);
      setError(null);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Nie udało się pobrać organizacji.");
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void loadTenant();
  }, []);

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!tenant) {
      return;
    }
    setSaving(true);
    setMessage(null);
    setError(null);
    try {
      const response = await api<TenantResponse>("/api/v1/admin/tenant/", {
        method: "PUT",
        body: jsonBody({ name, version: tenant.version })
      });
      setTenant(response.tenant);
      setName(response.tenant.name);
      setMessage("Nazwa organizacji została zaktualizowana.");
    } catch (cause) {
      if (cause instanceof ApiError && cause.status === 409) {
        setError("Rekord został zmieniony równolegle. Odświeżono bieżącą wersję.");
        await loadTenant();
      } else {
        setError(cause instanceof Error ? cause.message : "Aktualizacja nie powiodła się.");
      }
    } finally {
      setSaving(false);
    }
  }

  if (loading) {
    return <PageState title="Ładowanie organizacji" />;
  }
  if (!tenant) {
    return <PageState title="Nie można wyświetlić organizacji" detail={error ?? undefined} tone="error" />;
  }

  return (
    <section className="page">
      <div className="page-heading">
        <div>
          <p className="eyebrow">Administracja</p>
          <h1>Organizacja</h1>
          <p className="muted">Edycja używa optimistic locking przez pole version.</p>
        </div>
      </div>

      <div className="panel">
        <form className="form-stack form-stack--compact" onSubmit={(event) => void handleSubmit(event)}>
          <label>
            Nazwa organizacji
            <input value={name} onChange={(event) => setName(event.target.value)} maxLength={200} required />
          </label>
          <div className="detail-grid">
            <div><dt>Slug</dt><dd>{tenant.slug}</dd></div>
            <div><dt>Status</dt><dd>{tenant.status}</dd></div>
            <div><dt>Wersja</dt><dd>{tenant.version}</dd></div>
            <div><dt>Aktualizacja</dt><dd>{new Date(tenant.updated_at).toLocaleString("pl-PL")}</dd></div>
          </div>
          {error ? <div className="inline-error" role="alert">{error}</div> : null}
          {message ? <div className="inline-success" role="status">{message}</div> : null}
          <div>
            <button className="button button--primary" type="submit" disabled={saving || name.trim() === tenant.name}>
              {saving ? "Zapisywanie…" : "Zapisz zmianę"}
            </button>
          </div>
        </form>
      </div>
    </section>
  );
}
