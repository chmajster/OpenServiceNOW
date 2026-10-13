import { Link } from "react-router-dom";
import { useAuth } from "../auth/AuthContext";
import { useState } from "react";
import { useIncidentQuery } from "../incidents/hooks";
import { LoadState } from "../requests/LoadState";
import { useEditor } from "../requests/useEditor";
import { UnsavedGuard } from "../incidents/UnsavedGuard";
import { labels, errors, type Execution } from "./contracts";

export function RequestProcesses({ requestID }: { requestID: string }) {
  const page = useIncidentQuery<{ items: Execution[] }>(
    `/api/v1/workflow/executions?request_id=${requestID}&limit=100`,
  );
  return (
    <section className="panel">
      <h2>Procesy realizacji</h2>
      <LoadState
        loading={page.loading}
        error={page.error}
        retry={page.reload}
      />
      {page.data?.items.length ? (
        <ul>
          {page.data.items.map((x) => (
            <li key={x.id}>
              <Link to={`/approvals/${x.id}`}>
                {x.name} · wersja {x.definition_version}
              </Link>{" "}
              — {labels[x.state]} · krok {x.current_node}
            </li>
          ))}
        </ul>
      ) : !page.loading && !page.error ? (
        <p>Realizacja przez niezależne zadania ręczne.</p>
      ) : null}
      <button
        className="button button--secondary"
        onClick={page.reload}
        disabled={page.loading}
      >
        Odśwież procesy
      </button>
    </section>
  );
}
export function ProcessDetails({ id }: { id: string }) {
  const q = useIncidentQuery<Execution>(`/api/v1/workflow/executions/${id}`);
  return (
    <>
      <LoadState loading={q.loading} error={q.error} retry={q.reload} />
      {q.data ? <ProcessWorkspace key={id} initial={q.data} /> : null}
    </>
  );
}
function ProcessWorkspace({ initial }: { initial: Execution }) {
  const { hasPermission } = useAuth();
  const path = `/api/v1/workflow/executions/${initial.id}`,
    ed = useEditor(initial, path);
  const x = ed.record,
    [reason, setReason] = useState(""),
    [choice, setChoice] = useState("APPROVED");
  const pending = x.steps.find(
    (s) => s.node_id === x.current_node && s.can_decide,
  );
  async function decide() {
    if (!pending) return;
    const ok = await ed.save(`${path}/decisions/${pending.node_id}`, {
      version: pending.version,
      decision: choice,
      reason,
    });
    if (ok) setReason("");
  }
  async function repair(action: string) {
    const ok = await ed.save(`${path}/repair`, {
      version: x.version,
      action,
      reason,
    });
    if (ok) setReason("");
  }
  const disabled = ed.busy || !!ed.conflict;
  return (
    <>
      <UnsavedGuard dirty={!!reason} />
      <h1>
        {x.name} · {x.request_number}
      </h1>
      <p>
        Wersja procesu {x.definition_version} · {labels[x.state]} · krok{" "}
        {x.current_node} · wykonanie #{x.id}
      </p>
      <p className="action-row">
        {hasPermission("workflow.approve") ? (
          <Link to="/approvals">Moje akceptacje</Link>
        ) : null}
        {hasPermission("workflow.manage") ? (
          <Link to="/workflow?tab=executions">Wykonania procesów</Link>
        ) : null}
      </p>
      <p>
        Oczekiwanie jest trwałe. Zapis decyzji i dalsze wykonanie przez worker
        mogą nastąpić w różnych momentach.
      </p>
      <button
        className="button button--secondary"
        disabled={ed.busy || !!reason}
        onClick={async () => {
          const current = await ed.save(path, undefined, "GET");
          if (current) window.dispatchEvent(new Event("notifications-changed"));
        }}
      >
        Odśwież stan procesu
      </button>
      <LoadState error={ed.error} />
      {ed.notice ? <p role="status">{ed.notice}</p> : null}
      {x.error_code ? (
        <p role="alert">{errors[x.error_code] ?? x.error_code}</p>
      ) : null}
      {ed.conflict ? (
        <section className="conflict-panel">
          <h2>Konflikt decyzji lub wersji</h2>
          <p>
            Lokalna propozycja: {labels[choice]}; {reason}. Aktualny stan:{" "}
            {labels[ed.conflict.state]}, krok {ed.conflict.current_node}. Twoje
            uzasadnienie pozostaje w formularzu.
          </p>
          <details>
            <summary>Aktualny stan</summary>
            <pre>{JSON.stringify(ed.conflict, null, 2)}</pre>
          </details>
          <button className="button button--secondary" onClick={ed.resolve}>
            Porównano — użyj aktualnej wersji
          </button>
        </section>
      ) : null}
      {x.snapshot ? (
        <section className="panel">
          <h2>{x.snapshot.title}</h2>
          <dl>
            {x.snapshot.fields.map((f) =>
              Object.hasOwn(x.values ?? {}, f.key) ? (
                <div key={f.key}>
                  <dt>{f.label}</dt>
                  <dd>{String(x.values?.[f.key])}</dd>
                </div>
              ) : null,
            )}
          </dl>
        </section>
      ) : null}
      <ol className="workflow-steps" aria-label="Stan procesu">
        {x.steps.map((s) => (
          <li
            key={s.node_id}
            className={`workflow-step workflow-step--${s.state.toLowerCase()}`}
            aria-current={s.node_id === x.current_node ? "step" : undefined}
          >
            <h2>{s.label}</h2>
            <p>
              {labels[s.type]} · {labels[s.state]}
            </p>
            {s.deadline ? (
              <p>
                Termin:{" "}
                <time dateTime={s.deadline}>
                  {new Date(s.deadline).toLocaleString("pl-PL")}
                </time>
              </p>
            ) : null}
            {s.result?.outcome ? (
              <p>{labels[s.result.outcome] ?? s.result.outcome}</p>
            ) : null}
            {s.decisions?.map((d) => (
              <p key={d.user_id}>
                {d.name}:{" "}
                {d.decision === "PENDING" && s.state !== "WAITING"
                  ? "Decyzja niewymagana — krok zakończony"
                  : labels[d.decision]}
                {d.reason ? ` · ${d.reason}` : ""}
              </p>
            ))}
          </li>
        ))}
      </ol>
      {pending || x.actions.length ? (
        <form
          className="panel"
          onSubmit={(e) => {
            e.preventDefault();
            void decide();
          }}
        >
          <h2>{pending ? "Twoja decyzja" : "Operacja naprawcza"}</h2>
          <fieldset disabled={disabled}>
            {pending ? (
              <label>
                Decyzja
                <select
                  value={choice}
                  onChange={(e) => setChoice(e.target.value)}
                >
                  <option value="APPROVED">Akceptuję</option>
                  <option value="REJECTED">Odrzucam</option>
                </select>
              </label>
            ) : null}
            <label>
              Uzasadnienie
              <textarea
                aria-label="Uzasadnienie"
                required
                maxLength={2000}
                value={reason}
                onChange={(e) => setReason(e.target.value)}
              />
            </label>
            {pending ? (
              <button className="button button--primary" type="submit">
                Zapisz decyzję
              </button>
            ) : null}
            {x.actions.map((a) => (
              <button
                key={a}
                type="button"
                className="button button--secondary"
                disabled={!reason.trim()}
                onClick={() => {
                  if (
                    a === "CANCEL" &&
                    !window.confirm(
                      "Zatrzymać proces? Nie zamknie to automatycznie wniosku ani jego zadań.",
                    )
                  )
                    return;
                  void repair(a);
                }}
              >
                {a === "RESUME" ? "Wznów bieżący krok" : "Anuluj proces"}
              </button>
            ))}
          </fieldset>
        </form>
      ) : null}
      {x.attempts ? (
        <section className="panel">
          <h2>Historia prób (ostatnie 100)</h2>
          <div className="table-scroll">
            <table>
              <thead>
                <tr>
                  <th>Data</th>
                  <th>Krok</th>
                  <th>Wersja</th>
                  <th>Wynik</th>
                  <th>Przyczyna</th>
                </tr>
              </thead>
              <tbody>
                {x.attempts.map((a) => (
                  <tr key={a.id}>
                    <td>{new Date(a.occurred_at).toLocaleString("pl-PL")}</td>
                    <td>{a.node_id}</td>
                    <td>{a.execution_version}</td>
                    <td>{labels[a.outcome] ?? a.outcome}</td>
                    <td>
                      {a.error_code
                        ? (errors[a.error_code] ?? a.error_code)
                        : "—"}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </section>
      ) : null}
    </>
  );
}
