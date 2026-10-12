import { useEffect, useRef, useState } from "react";
import { api, jsonBody } from "../api/client";
import type { IncidentSummary } from "./contracts";
export type Selection = Record<
  string,
  { id: string; version: string; number: string }
>;
type Result = { id: string; status: number; message: string };
export function QueueActions({
  selection,
  clear,
  refresh,
  canAssign,
  canStart,
  loading,
  busy,
  setBusy,
}: {
  selection: Selection;
  clear: () => void;
  refresh: () => void;
  canAssign: boolean;
  canStart: boolean;
  loading: boolean;
  busy: boolean;
  setBusy: (busy: boolean) => void;
}) {
  const [results, setResults] = useState<Result[]>([]),
    [error, setError] = useState<string>();
  const mounted = useRef(true),
    request = useRef<AbortController | null>(null);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
      request.current?.abort();
      setBusy(false);
    };
  }, [setBusy]);
  async function run(action: string) {
    const items = Object.values(selection);
    if (
      !items.length ||
      busy ||
      !window.confirm(
        `${action === "assign_self" ? "Przypisać do siebie" : "Rozpocząć obsługę"} ${items.length} wybranych incydentów? Każdy zapis sprawdzi własną wersję i uprawnienia.`,
      )
    )
      return;
    setBusy(true);
    setError(undefined);
    request.current = new AbortController();
    try {
      const response = await api<{ results: Result[] }>(
        "/api/v1/incidents/batch",
        {
          method: "POST",
          body: jsonBody({
            action,
            items: items.map(({ id, version }) => ({ id, version })),
          }),
          signal: request.current.signal,
        },
      );
      if (mounted.current) {
        setResults(response.results);
        clear();
        refresh();
      }
    } catch (cause) {
      if (mounted.current) {
        setError(
          (cause instanceof Error
            ? cause.message
            : "Nie potwierdzono operacji.") +
            " Odczytaj rekordy przed ponowieniem; część zapisów mogła się zakończyć.",
        );
        clear();
        refresh();
      }
    } finally {
      if (mounted.current) setBusy(false);
    }
  }
  return (
    <div className="queue-toolbar">
      <strong>Wybrano: {Object.keys(selection).length}</strong>
      {canAssign && (
        <button
          className="button button--primary"
          disabled={busy || loading || !Object.keys(selection).length}
          onClick={() => void run("assign_self")}
        >
          Przypisz wybrane do siebie
        </button>
      )}
      {canStart && (
        <button
          className="button button--secondary"
          disabled={busy || loading || !Object.keys(selection).length}
          onClick={() => void run("start")}
        >
          Rozpocznij obsługę wybranych
        </button>
      )}
      <button
        className="button button--secondary"
        disabled={busy}
        onClick={clear}
      >
        Wyczyść wybór
      </button>
      {busy && (
        <p role="status">Sprawdzanie i zapisywanie poszczególnych rekordów…</p>
      )}
      {error && (
        <p className="notice notice--error" role="alert">
          {error}
        </p>
      )}
      {!!results.length && (
        <div className="queue-results" role="status">
          <h3>Wyniki operacji</h3>
          <ul>
            {results.map((r) => (
              <li key={r.id}>
                Incydent {r.id}:{" "}
                {r.status === 200 ? "Zapisano" : `HTTP ${r.status}`} ·{" "}
                {r.message}
              </li>
            ))}
          </ul>
          <p>Przed kolejną akcją wybierz rekordy z aktualnej listy.</p>
        </div>
      )}
    </div>
  );
}
export function selectedRecord(r: IncidentSummary) {
  return { id: r.id, version: r.version, number: r.number };
}
