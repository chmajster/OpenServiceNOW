import { useEffect, useRef, useState } from "react";
import { Link } from "react-router-dom";
import { api, ApiError, jsonBody } from "../api/client";
import { useAuth } from "../auth/AuthContext";
import { InventoryChoice, More, PageFeedback } from "../cmdb/components";
import { useRemotePage } from "../components/useRemotePage";
import { useIncidentQuery } from "../incidents/hooks";
import type { Choice } from "../components/RemoteCombobox";
import { errorText, labels, type Problem } from "./contracts";
export function IncidentProblems({
  record,
  onChanged,
}: {
  record: { id: string; version: string };
  onChanged: () => Promise<void>;
}) {
  const { hasPermission } = useAuth(),
    p = useRemotePage<Problem>(
      `/api/v1/problems/?incident_id=${record.id}&limit=25`,
      "items",
      "before_id",
    ),
    [choice, setChoice] = useState<Choice | null>(null),
    selected = useIncidentQuery<Problem>(
      choice ? `/api/v1/problems/${choice.id}` : null,
    );
  const [busy, setBusy] = useState(false),
    [error, setError] = useState(""),
    [notice, setNotice] = useState(""),
    [uncertain, setUncertain] = useState(false),
    operation = useRef<
      { path: string; body: string; method: string; key: string } | undefined
    >(undefined),
    alive = useRef(true),
    controller = useRef<AbortController | undefined>(undefined);
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
      controller.current?.abort();
    };
  }, []);
  async function change(problem: Problem | undefined, remove: boolean) {
    if (busy || (!problem && !operation.current)) return;
    setBusy(true);
    setError("");
    const c = new AbortController();
    controller.current = c;
    try {
      const proposed =
        uncertain && operation.current
          ? operation.current
          : {
              path: `/api/v1/problems/${problem!.id}/incidents/${record.id}`,
              method: remove ? "DELETE" : "PUT",
              body: jsonBody({
                version: problem!.version,
                incident_version: record.version,
              }),
              key: crypto.randomUUID(),
            };
      operation.current = proposed;
      await api(proposed.path, {
        method: proposed.method,
        body: proposed.body,
        headers: { "Idempotency-Key": proposed.key },
        signal: c.signal,
      });
      if (!alive.current) return;
      setUncertain(false);
      operation.current = undefined;
      setChoice(null);
      p.reload();
      setNotice("Zapisano powiązanie problemu.");
      await onChanged();
    } catch (e) {
      if (alive.current && !c.signal.aborted) {
        setError(errorText(e));
        setUncertain(!(e instanceof ApiError) || e.status >= 500);
      }
    } finally {
      if (alive.current) setBusy(false);
    }
  }
  return (
    <>
      <h3>Problemy powiązane z incydentem</h3>
      <p>Rozwiązanie problemu zachowuje stan tego incydentu.</p>
      <button
        disabled={busy || uncertain}
        onClick={() => {
          p.reload();
          selected.reload();
          void onChanged();
        }}
      >
        Odśwież powiązania problemów
      </button>
      <PageFeedback
        loading={p.loading}
        error={p.error}
        empty={!p.items.length}
      />
      {error && <p role="alert">{error} Odśwież rekordy przed ponowieniem.</p>}
      {notice && <p role="status">{notice}</p>}
      {uncertain && (
        <button disabled={busy} onClick={() => void change(undefined, false)}>
          Potwierdź wynik powiązania
        </button>
      )}
      <ul>
        {p.items.map((pr) => (
          <li key={pr.id}>
            <Link to={`/problems/${pr.id}`}>
              {pr.number} · {pr.title}
            </Link>{" "}
            · {labels[pr.state]}
            {pr.can_edit && (
              <button
                disabled={busy || uncertain}
                onClick={() => void change(pr, true)}
              >
                Odłącz problem
              </button>
            )}
          </li>
        ))}
      </ul>
      <More loading={p.loading} next={p.next} load={p.more} />
      {hasPermission("problem.create") && (
        <Link
          className="button"
          to={`/problems/new?source_incident_id=${record.id}`}
        >
          Utwórz problem z incydentu
        </Link>
      )}
      {hasPermission("problem.manage") && (
        <form
          onSubmit={(e) => {
            e.preventDefault();
            if (selected.data) void change(selected.data, false);
          }}
        >
          <InventoryChoice
            label="Istniejący problem"
            endpoint="/api/v1/problems/"
            value={choice}
            onChange={setChoice}
            disabled={busy || uncertain}
          />
          <PageFeedback
            loading={!!choice && selected.loading}
            error={selected.error}
            empty={false}
          />
          {selected.data && (
            <p>
              Podgląd:{" "}
              <Link to={`/problems/${selected.data.id}`}>
                {selected.data.number} · {selected.data.title}
              </Link>{" "}
              · {labels[selected.data.state]} · Wersja {selected.data.version}
            </p>
          )}
          <button disabled={busy || uncertain || !selected.data?.can_edit}>
            Powiąż istniejący problem
          </button>
        </form>
      )}
    </>
  );
}
