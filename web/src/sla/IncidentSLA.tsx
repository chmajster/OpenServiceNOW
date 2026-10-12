import { useSearchParams } from "react-router-dom";
import { PageState } from "../components/PageState";
import { useIncidentQuery } from "../incidents/hooks";
import { dateLabel } from "../incidents/contracts";
import {
  kindLabel,
  statusLabel,
  workingTime,
  type Instance,
  type InstancePage,
  type HistoryPage,
} from "./contracts";

export function SLASummary({ items }: { items: Instance[] }) {
  if (!items.length)
    return <span className="muted">Brak dopasowanego SLA</span>;
  return (
    <div className="sla-summary">
      {items.map((i) => (
        <div key={i.id}>
          <strong>{kindLabel(i.kind)}</strong>:{" "}
          {i.breached_at
            ? `Naruszenie — ${statusLabel(i.status)}`
            : statusLabel(i.status)}
          <div>
            {i.status === "PAUSED"
              ? `Powód: ${i.pause_reason}`
              : i.due_at
                ? dateLabel(i.due_at)
                : "Brak terminu"}
          </div>
          <small>{workingTime(i.remaining_seconds)}</small>
        </div>
      ))}
    </div>
  );
}
export function IncidentSLA({ id, version }: { id: string; version: string }) {
  const result = useIncidentQuery<InstancePage>(
    `/api/v1/sla/incidents?ids=${id}`,
    version,
  );
  const [params, setParams] = useSearchParams();
  const before = params.get("sla_before") ?? "";
  const history = useIncidentQuery<HistoryPage>(
    `/api/v1/sla/incidents/${id}/history?before_id=${before}`,
    version,
  );
  return (
    <section className="panel sla-panel" aria-label="SLA incydentu">
      <div className="page-heading">
        <h2>Poziom obsługi — SLA</h2>
        <button
          type="button"
          className="button button--secondary"
          disabled={result.loading || history.loading}
          onClick={() => {
            result.reload();
            history.reload();
          }}
        >
          Odśwież SLA
        </button>
      </div>
      {result.loading ? (
        <PageState title="Ładowanie SLA…" />
      ) : result.error ? (
        <PageState
          title="Nie udało się odczytać SLA"
          detail={result.error}
          tone="error"
        />
      ) : (
        result.data && (
          <>
            {result.data.pending && (
              <p role="status" className="notice">
                Stan SLA oczekuje na przetworzenie przez worker lub obliczenie
                jest opóźnione. Odśwież widok za chwilę.
              </p>
            )}
            <SLASummary items={result.data.items} />
            {result.data.items.map((i) => (
              <article className="sla-card" key={i.id}>
                <h3>
                  {kindLabel(i.kind)} — {statusLabel(i.status)}
                </h3>
                <dl className="detail-grid">
                  <div>
                    <dt>Polityka / cykl</dt>
                    <dd>
                      {i.policy_name} v{i.policy_version} / {i.cycle}
                    </dd>
                  </div>
                  <div>
                    <dt>Kalendarz</dt>
                    <dd>
                      #{i.calendar_id} v{i.calendar_version}
                    </dd>
                  </div>
                  <div>
                    <dt>Start</dt>
                    <dd>{dateLabel(i.started_at)}</dd>
                  </div>
                  <div>
                    <dt>Naliczono</dt>
                    <dd>{workingTime(i.accrued_seconds)}</dd>
                  </div>
                  <div>
                    <dt>Ostrzeżenie</dt>
                    <dd>{dateLabel(i.warned_at)}</dd>
                  </div>
                  <div>
                    <dt>Naruszenie</dt>
                    <dd>{dateLabel(i.breached_at)}</dd>
                  </div>
                  <div>
                    <dt>Zakończenie</dt>
                    <dd>{dateLabel(i.finished_at)}</dd>
                  </div>
                  <div>
                    <dt>Stan obliczony na</dt>
                    <dd>
                      {dateLabel(i.calculated_at)} — wersja incydentu{" "}
                      {i.incident_version}
                    </dd>
                  </div>
                </dl>
              </article>
            ))}
            <details>
              <summary>Zasady naliczania</summary>
              <p>{result.data.rules}</p>
              <p>
                Rozwiązanie bez publicznej odpowiedzi kończy miernik odpowiedzi
                jako „Zakończone bez odpowiedzi”. Pozostały czas pochodzi z
                ostatniego obliczenia workera, także po zamknięciu przeglądarki.
              </p>
            </details>
          </>
        )
      )}
      <h3>Historia SLA</h3>
      {history.loading ? (
        <p role="status">Ładowanie historii…</p>
      ) : history.error ? (
        <p role="alert">{history.error}</p>
      ) : !history.data?.items.length ? (
        <p className="muted">Brak historii SLA.</p>
      ) : (
        <ol className="activity-list">
          {history.data.items.map((e) => (
            <li key={e.id}>
              <strong>
                {{
                  started: "Rozpoczęto",
                  paused: "Wstrzymano",
                  resumed: "Wznowiono",
                  finished: "Zakończono",
                  warning: "Ostrzeżenie",
                  breached: "Naruszenie",
                  priority_preserved: "Zachowano pierwotną politykę",
                  pause_reason_changed: "Zmieniono powód pauzy",
                }[e.type] ?? e.type}
              </strong>{" "}
              — {dateLabel(e.occurred_at)}
              <p>
                {kindLabel(e.kind)} — cykl {e.cycle}, SLA #{e.instance_id},
                wersja incydentu {e.incident_version}; naliczono{" "}
                {workingTime(e.accrued_seconds)}. {e.reason}
              </p>
              <p>Termin: {dateLabel(e.due_at)}</p>
              <details>
                <summary>Źródło techniczne</summary>
                <p>
                  Zdarzenie {e.source_event_id}, wpis {e.id}
                </p>
              </details>
            </li>
          ))}
        </ol>
      )}
      <div className="action-row">
        {before && (
          <button
            className="button button--secondary"
            onClick={() => {
              const p = new URLSearchParams(params);
              p.delete("sla_before");
              setParams(p);
            }}
          >
            Najnowsza historia SLA
          </button>
        )}
        {history.data?.next_before_id && (
          <button
            className="button button--secondary"
            onClick={() => {
              const p = new URLSearchParams(params);
              p.set("sla_before", history.data!.next_before_id!);
              setParams(p);
            }}
          >
            Starsza historia SLA
          </button>
        )}
      </div>
    </section>
  );
}
