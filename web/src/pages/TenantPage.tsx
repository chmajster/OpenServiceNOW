import { useEffect, useState, type FormEvent } from "react";
import { ApiError, api, jsonBody } from "../api/client";
import type { Tenant, TenantResponse } from "../api/types";
import { ConflictNotice } from "../components/ConflictNotice";
import { PageState } from "../components/PageState";
import { useAuth } from "../auth/AuthContext";

export function TenantPage() {
  const { hasPermission, refresh } = useAuth();
  const canManage = hasPermission("tenant.manage");
  const [tenant, setTenant] = useState<Tenant | null>(null);
  const [name, setName] = useState("");
  const [saving, setSaving] = useState(false);
  const [retry, setRetry] = useState(0);
  const [message, setMessage] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [conflict, setConflict] = useState<Tenant | null>(null);
  const [unresolved, setUnresolved] = useState(false);
  useEffect(() => {
    const controller = new AbortController();
    void api<TenantResponse>("/api/v1/admin/tenant/", {
      signal: controller.signal,
    })
      .then((response) => {
        if (!controller.signal.aborted) {
          setTenant(response.tenant);
          setName(response.tenant.name);
          setError(null);
        }
      })
      .catch((cause: unknown) => {
        if (!controller.signal.aborted)
          setError(
            cause instanceof Error
              ? cause.message
              : "Nie udało się pobrać organizacji.",
          );
      });
    return () => controller.abort();
  }, [retry]);
  async function readConflict() {
    setSaving(true);
    try {
      const result = await api<TenantResponse>("/api/v1/admin/tenant/");
      setConflict(result.tenant);
      setError(null);
    } catch (cause) {
      setError(
        cause instanceof Error
          ? cause.message
          : "Nie udało się pobrać aktualnego stanu.",
      );
    } finally {
      setSaving(false);
    }
  }
  async function submit(event: FormEvent) {
    event.preventDefault();
    if (!tenant || !canManage || saving || unresolved) return;
    setSaving(true);
    setMessage(null);
    setError(null);
    try {
      const response = await api<TenantResponse>("/api/v1/admin/tenant/", {
        method: "PUT",
        body: jsonBody({ name, version: tenant.version }),
      });
      setTenant(response.tenant);
      setName(response.tenant.name);
      setMessage("Nazwa organizacji została zaktualizowana.");
      await refresh();
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
      setSaving(false);
    }
  }
  function resolve(useCurrent: boolean) {
    if (!conflict) return;
    setTenant(conflict);
    if (useCurrent) setName(conflict.name);
    setConflict(null);
    setUnresolved(false);
  }
  if (!tenant)
    return (
      <>
        <PageState
          title={
            error ? "Nie można wyświetlić organizacji" : "Ładowanie organizacji"
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
    <section className="page">
      <div className="page-heading">
        <div>
          <p className="eyebrow">Administracja</p>
          <h1>Organizacja</h1>
          <p className="muted">Dane bieżącej organizacji.</p>
        </div>
      </div>
      <div className="panel">
        <form className="form-stack" onSubmit={(event) => void submit(event)}>
          <label>
            Nazwa organizacji
            <input
              value={name}
              onChange={(event) => setName(event.target.value)}
              maxLength={200}
              disabled={!canManage || saving}
              required
            />
          </label>
          <dl className="detail-grid">
            <div>
              <dt>Identyfikator organizacji</dt>
              <dd>{tenant.slug}</dd>
            </div>
            <div>
              <dt>Status</dt>
              <dd>{tenant.status === "ACTIVE" ? "Aktywna" : "Wyłączona"}</dd>
            </div>
            <div>
              <dt>Wersja</dt>
              <dd>{tenant.version}</dd>
            </div>
            <div>
              <dt>Aktualizacja</dt>
              <dd>{new Date(tenant.updated_at).toLocaleString("pl-PL")}</dd>
            </div>
          </dl>
          {!canManage ? (
            <PageState
              title="Tryb tylko do odczytu"
              detail="Nie masz uprawnienia do zmiany organizacji."
            />
          ) : null}
          {error ? (
            <div className="inline-error" role="alert">
              {error}
            </div>
          ) : null}
          {message ? (
            <div className="inline-success" role="status">
              {message}
            </div>
          ) : null}
          {conflict ? (
            <ConflictNotice
              current={<p>{conflict.name}</p>}
              proposal={<p>{name}</p>}
              acceptCurrent={() => resolve(true)}
              keepProposal={() => resolve(false)}
            />
          ) : unresolved ? (
            <div role="alert">
              Twoja propozycja została zachowana. Pobierz aktualny stan przed
              rozstrzygnięciem konfliktu.{" "}
              <button
                type="button"
                className="button button--secondary"
                disabled={saving}
                onClick={() => void readConflict()}
              >
                Pobierz aktualny stan
              </button>
            </div>
          ) : null}
          <div>
            <button
              className="button button--primary"
              disabled={
                !canManage ||
                saving ||
                unresolved ||
                !name.trim() ||
                name.trim() === tenant.name
              }
            >
              {saving ? "Zapisywanie…" : "Zapisz zmianę"}
            </button>
          </div>
        </form>
      </div>
    </section>
  );
}
