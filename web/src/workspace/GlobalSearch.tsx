import { useState } from "react";
import { Link } from "react-router-dom";
import { useIncidentQuery } from "../incidents/hooks";

import { type Reference, referenceType, referenceState } from "./references";
export function GlobalSearch() {
  const [query, setQuery] = useState("");
  const [submitted, setSubmitted] = useState("");
  const [open, setOpen] = useState(false);
  const results = useIncidentQuery<{ items: Reference[] }>(
    submitted && open
      ? `/api/v1/workspace/search?q=${encodeURIComponent(submitted)}`
      : null,
  );
  return (
    <div
      className="global-search"
      onKeyDown={(e) => {
        if (e.key === "Escape") {
          setOpen(false);
          e.currentTarget.querySelector("input")?.focus();
        }
      }}
    >
      <form
        role="search"
        onSubmit={(e) => {
          e.preventDefault();
          if (submitted === query.trim() && open) results.reload();
          setSubmitted(query.trim());
          setOpen(true);
        }}
      >
        <label className="sr-only" htmlFor="global-query">
          Wyszukaj rekordy
        </label>
        <input
          id="global-query"
          type="search"
          placeholder="Szukaj numeru lub opisu…"
          minLength={2}
          maxLength={200}
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          aria-controls={open ? "global-results" : undefined}
        />
        <button
          className="button button--small"
          disabled={query.trim().length < 2}
        >
          Szukaj
        </button>
      </form>
      {open && (
        <section
          id="global-results"
          className="search-results"
          aria-label="Wyniki wyszukiwania"
        >
          <div className="panel-heading">
            <h2>Wyniki: {submitted}</h2>
            <button
              className="button button--small button--secondary"
              onClick={() => setOpen(false)}
            >
              Zamknij wyniki
            </button>
          </div>
          {results.loading ? (
            <p role="status">Wyszukiwanie…</p>
          ) : results.error ? (
            <div role="alert">
              <p>{results.error}</p>
              <button
                className="button button--secondary"
                onClick={results.reload}
              >
                Spróbuj ponownie
              </button>
            </div>
          ) : !results.data?.items.length ? (
            <p role="status">Brak dostępnych rekordów.</p>
          ) : (
            <ul>
              {results.data.items.map((r) => (
                <li key={`${r.type}:${r.id}`}>
                  <Link to={r.url} onClick={() => setOpen(false)}>
                    <span className="muted">{referenceType(r)}</span>{" "}
                    <strong>{r.number}</strong>
                    {r.exact && (
                      <span className="tag">
                        {["ci", "catalog_item", "workflow_execution"].includes(
                          r.type,
                        )
                          ? "Dokładny identyfikator"
                          : "Dokładny numer"}
                      </span>
                    )}
                    <span>{r.title}</span>
                    <small>{referenceState(r)}</small>
                  </Link>
                </li>
              ))}
            </ul>
          )}
          <small className="muted">
            Do 10 wyników każdego typu. Dalsze wyniki znajdziesz w wyszukiwaniu
            modułu.
          </small>
        </section>
      )}
    </div>
  );
}
