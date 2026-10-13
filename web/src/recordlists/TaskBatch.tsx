import { useState } from "react";
import { api, jsonBody } from "../api/client";
export function TaskBatch({
  selection,
  clear,
  refresh,
  busy,
  setBusy,
}: {
  selection: { id: string; version: string }[];
  clear: () => void;
  refresh: () => void;
  busy: boolean;
  setBusy: (v: boolean) => void;
}) {
  const [results, setResults] = useState<
      { id: string; status: number; message: string }[]
    >([]),
    [error, setError] = useState("");
  async function run(operation: string) {
    if (busy || !selection.length) return;
    setBusy(true);
    setError("");
    setResults([]);
    try {
      const result = await api<{
        results: { id: string; status: number; message: string }[];
      }>("/api/v1/fulfillment/batch", {
        method: "POST",
        body: jsonBody({ operation, records: selection }),
      });
      setResults(result.results);
      clear();
      refresh();
    } catch (cause) {
      setError(
        cause instanceof Error
          ? cause.message
          : "Nie potwierdzono operacji. Odczytaj aktualne wersje przed ponowieniem.",
      );
    } finally {
      setBusy(false);
    }
  }
  return (
    <div className="task-batch">
      <div className="action-row">
        <strong>Zaznaczono: {selection.length}</strong>
        <button
          className="button button--secondary"
          disabled={busy || selection.length === 0 || selection.length > 50}
          onClick={() => void run("ASSIGN_SELF")}
        >
          Przypisz zaznaczone do mnie
        </button>
        <button
          className="button button--secondary"
          disabled={busy || selection.length === 0 || selection.length > 50}
          onClick={() => void run("START")}
        >
          Rozpocznij przypisane zadania
        </button>
        <button
          className="button button--secondary"
          disabled={busy}
          onClick={clear}
        >
          Wyczyść zaznaczenie
        </button>
      </div>
      {error && <p role="alert">{error}</p>}
      {results.length > 0 && (
        <section role="status" aria-label="Wyniki operacji masowej">
          <h3>Wyniki dla każdego zadania</h3>
          <ul>
            {results.map((r) => (
              <li key={r.id}>
                #{r.id}: {r.message} ({r.status})
              </li>
            ))}
          </ul>
        </section>
      )}
    </div>
  );
}
