import { useEffect, useRef, useState } from "react";
import { flushSync } from "react-dom";
import {
  Link,
  useNavigate,
  useParams,
  useSearchParams,
} from "react-router-dom";
import { api, ApiError } from "../api/client";
import { useAuth } from "../auth/AuthContext";
import { useRemotePage } from "../components/useRemotePage";
import { LoadState } from "../requests/LoadState";
import { useIncidentQuery, useIncidentTitleFocus } from "../incidents/hooks";
import { UnsavedGuard } from "../incidents/UnsavedGuard";
import { ServiceForm } from "../requests/ServiceForm";
import {
  cleanValues,
  message,
  type Node,
  type Publication,
  type Request,
  type Values,
} from "../requests/contracts";
export function CatalogPage() {
  const { itemID } = useParams();
  return itemID ? <Order key={itemID} id={itemID} /> : <Browse />;
}
function Browse() {
  const heading = useIncidentTitleFocus();
  const { hasPermission } = useAuth();
  const [params, set] = useSearchParams();
  const q = params.get("q") ?? "",
    category = params.get("category_id") ?? "",
    catalogID = params.get("catalog_id") ?? "";
  const page = useRemotePage<Publication>(
    `/api/v1/catalog/items?limit=25&q=${encodeURIComponent(q)}&category_id=${category}&catalog_id=${catalogID}`,
    "items",
  );
  const categories = useRemotePage<Node>(
    "/api/v1/catalog/categories?limit=100",
    "items",
  );
  const catalogs = useRemotePage<Node>(
    "/api/v1/catalog/catalogs?limit=100",
    "items",
  );
  if (!hasPermission("catalog.read"))
    return <LoadState error="Brak uprawnień do katalogu." />;
  return (
    <>
      <h1 ref={heading} tabIndex={-1}>
        Katalog usług
      </h1>
      <p>
        Wybierz usługę i wypełnij jej formularz.{" "}
        <Link to="/requests">Moje wnioski</Link>
      </p>
      <div className="panel form-grid">
        <label>
          Katalog
          <select
            aria-label="Katalog"
            value={catalogID}
            onChange={(e) => {
              const n = new URLSearchParams(params);
              n.set("catalog_id", e.target.value);
              n.delete("category_id");
              set(n);
            }}
          >
            <option value="">Wszystkie katalogi</option>
            {catalogs.items.map((n) => (
              <option key={n.id} value={n.id}>
                {n.name}
              </option>
            ))}
          </select>
        </label>
        {catalogs.next ? (
          <button
            className="button button--secondary"
            disabled={catalogs.loading}
            onClick={() => void catalogs.more()}
          >
            Więcej katalogów
          </button>
        ) : null}
        <label>
          Wyszukaj usługę
          <input
            value={q}
            maxLength={200}
            onChange={(e) => {
              const n = new URLSearchParams(params);
              n.set("q", e.target.value);
              set(n, { replace: true });
            }}
          />
        </label>
        <label>
          Kategoria
          <select
            value={category}
            onChange={(e) => {
              const n = new URLSearchParams(params);
              n.set("category_id", e.target.value);
              set(n);
            }}
          >
            <option value="">Wszystkie kategorie</option>
            {categories.items
              .filter((n) => !catalogID || n.catalog_id === catalogID)
              .map((n) => (
                <option key={n.id} value={n.id}>
                  {n.name}
                </option>
              ))}
          </select>
        </label>
        {categories.next ? (
          <button
            className="button button--secondary"
            disabled={categories.loading}
            onClick={() => void categories.more()}
          >
            Więcej kategorii
          </button>
        ) : null}
      </div>
      <LoadState
        loading={catalogs.loading}
        error={catalogs.error}
        retry={catalogs.retry}
      />
      <LoadState
        loading={categories.loading}
        error={categories.error}
        retry={categories.retry}
      />
      <LoadState loading={page.loading} error={page.error} retry={page.retry} />
      {!page.loading && !page.error && !page.items.length ? (
        <p role="status">Brak dostępnych usług.</p>
      ) : null}
      <div className="catalog-cards">
        {page.items.map((i) => (
          <section className="panel" key={i.id}>
            <h2>
              <Link to={`/catalog/${i.id}`}>{i.title}</Link>
            </h2>
            <p className="preserve-lines">{i.description}</p>
            <small>Wersja formularza {i.version}</small>
          </section>
        ))}
      </div>
      {page.next ? (
        <button
          className="button button--secondary"
          disabled={page.loading}
          onClick={() => void page.more()}
        >
          Więcej usług
        </button>
      ) : null}
    </>
  );
}
function Order({ id }: { id: string }) {
  const { hasPermission } = useAuth();
  const query = useIncidentQuery<Publication>(`/api/v1/catalog/items/${id}`);
  const heading = useIncidentTitleFocus(query.data?.version);
  const navigate = useNavigate();
  const [values, setValues] = useState<Values>({}),
    [busy, setBusy] = useState(false),
    [error, setError] = useState<string>(),
    [conflict, setConflict] = useState<Publication>();
  const proposal = useRef<{ key: string; body: string } | undefined>(undefined);
  const [confirmed, setConfirmed] = useState(false);
  const [uncertain, setUncertain] = useState(false);
  const alive = useRef(true),
    controller = useRef<AbortController | undefined>(undefined);
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
      controller.current?.abort();
    };
  }, []);
  const dirty = Object.keys(values).length > 0 && !confirmed;
  async function submit() {
    if (!query.data || busy || conflict) return;
    setBusy(true);
    setError(undefined);
    const body = JSON.stringify({
      items: [
        {
          item_id: id,
          version: query.data.version,
          values: cleanValues(query.data.fields, values),
        },
      ],
    });
    if (!uncertain && (!proposal.current || proposal.current.body !== body))
      proposal.current = { key: crypto.randomUUID(), body };
    const attempt = proposal.current!;
    const c = new AbortController();
    controller.current = c;
    try {
      const r = await api<Request>("/api/v1/requests/", {
        method: "POST",
        headers: { "Idempotency-Key": attempt.key },
        body: attempt.body,
        signal: c.signal,
      });
      if (!alive.current) return;
      flushSync(() => {
        setConfirmed(true);
        setValues({});
      });
      navigate(`/requests/${r.id}`);
    } catch (e) {
      if (!alive.current || c.signal.aborted) return;
      setError(message(e));
      setUncertain(!(e instanceof ApiError) || e.status >= 500);
      if (e instanceof ApiError && e.status < 500) proposal.current = undefined;
      if (
        e instanceof ApiError &&
        e.status === 409 &&
        e.code === "VERSION_CONFLICT"
      ) {
        try {
          const current = await api<Publication>(
            `/api/v1/catalog/items/${id}`,
            { signal: c.signal },
          );
          if (alive.current) setConflict(current);
        } catch (c) {
          setError(`${message(e)} ${message(c)}`);
        }
      }
    } finally {
      if (alive.current) setBusy(false);
    }
  }
  return (
    <>
      <h1 ref={heading} tabIndex={-1}>
        {query.data?.title ?? "Usługa"}
      </h1>
      <Link to="/catalog">Katalog usług</Link>
      <LoadState
        loading={query.loading}
        error={query.error}
        retry={query.reload}
      />
      <UnsavedGuard dirty={dirty} />
      {query.data ? (
        <form
          className="panel"
          onSubmit={(e) => {
            e.preventDefault();
            void submit();
          }}
        >
          <p>{query.data.description}</p>
          <p>Wersja formularza {query.data.version}</p>
          <ServiceForm
            fields={query.data.fields}
            values={values}
            onChange={setValues}
            disabled={busy || !!conflict || uncertain}
          />
          {error ? <p role="alert">{error}</p> : null}
          {uncertain ? (
            <p role="status">
              Nie potwierdzono wyniku poprzedniej próby. Ponowienie sprawdzi ten
              sam wniosek bez tworzenia duplikatu.
            </p>
          ) : null}
          {conflict ? (
            <section className="conflict-panel">
              <h2>Zmieniła się opublikowana wersja</h2>
              <p>
                Aktualna wersja: {conflict.version}. Twoja propozycja pozostaje
                poniżej. Porównaj nowe pola i przenieś wartości świadomie.
              </p>
              <pre>{JSON.stringify(values, null, 2)}</pre>
              <details>
                <summary>Aktualny formularz</summary>
                <pre>{JSON.stringify(conflict.fields, null, 2)}</pre>
              </details>
              <button
                type="button"
                className="button button--secondary"
                onClick={() => {
                  setValues(cleanValues(conflict.fields, values));
                  setConflict(undefined);
                  proposal.current = undefined;
                  query.reload();
                }}
              >
                Użyj aktualnego formularza z moimi wartościami
              </button>
            </section>
          ) : null}
          <button
            className="button button--primary"
            disabled={
              busy ||
              !!conflict ||
              !hasPermission("request.create") ||
              !hasPermission("request.read_own")
            }
          >
            {busy
              ? "Składanie…"
              : uncertain
                ? "Potwierdź wynik poprzedniej próby"
                : "Złóż wniosek"}
          </button>
          {!hasPermission("request.create") ? (
            <p>Brak uprawnienia do składania wniosku.</p>
          ) : null}
        </form>
      ) : null}
    </>
  );
}
