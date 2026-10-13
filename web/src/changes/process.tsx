import { Link } from "react-router-dom";
import { useIncidentQuery } from "../incidents/hooks";
import { LoadState } from "../requests/LoadState";
import type { Execution } from "../workflow/contracts";
import { labels, type Change, type Collision } from "./contracts";

export function ChangeApprovals({ record }: { record: Change }) {
  const p = useIncidentQuery<Execution>(
    record.execution_id
      ? `/api/v1/workflow/executions/${record.execution_id}`
      : null,
  );
  return (
    <section className="panel">
      <h2>Ocena i akceptacje zmiany</h2>
      <p>
        {labels[record.approval_state]} · Zakres oceny{" "}
        {record.assessment_revision} · Oceniana wersja{" "}
        {record.evaluated_version ?? "brak"}
      </p>
      {!record.execution_id ? (
        <p>
          Przejście do Autoryzacji uruchomi przypięty proces po sprawdzeniu
          planów, terminu i CI.
        </p>
      ) : (
        <>
          <LoadState loading={p.loading} error={p.error} retry={p.reload} />
          {p.data && (
            <>
              <Link className="button" to={`/approvals/${p.data.id}`}>
                Otwórz oceniany zakres i decyzje
              </Link>
              <p>
                {p.data.name} · Wersja procesu {p.data.definition_version} ·{" "}
                {p.data.state}
              </p>
              <ol>
                {p.data.steps
                  .filter((s) => s.type === "APPROVAL")
                  .map((s) => (
                    <li key={s.node_id}>
                      {s.label} · {s.state}
                      {s.deadline && (
                        <>
                          {" "}
                          · Termin{" "}
                          <time dateTime={s.deadline}>
                            {new Date(s.deadline).toLocaleString("pl-PL", {
                              timeZone: record.time_zone,
                            })}{" "}
                            ({record.time_zone})
                          </time>
                        </>
                      )}
                      <ul>
                        {s.decisions?.map((d) => (
                          <li key={d.user_id}>
                            {d.name}: {labels[d.decision] ?? d.decision}
                            {d.reason && ` · ${d.reason}`}
                          </li>
                        ))}
                      </ul>
                    </li>
                  ))}
              </ol>
            </>
          )}
        </>
      )}
    </section>
  );
}
export function ChangeCollisions({ record }: { record: Change }) {
  const p = useIncidentQuery<Collision>(
    `/api/v1/changes/${record.id}/collisions`,
  );
  return (
    <section className="panel">
      <h3>Sprawdzenie zapisanego terminu</h3>
      <LoadState loading={p.loading} error={p.error} retry={p.reload} />
      {p.data && (
        <>
          {p.data.items.length > 0 && (
            <ul>
              {p.data.items.map((c) => (
                <li key={c.id}>
                  Kolizja:{" "}
                  <Link to={`/changes/${c.id}`}>
                    {c.number} · {c.title}
                  </Link>
                </li>
              ))}
            </ul>
          )}
          {p.data.restricted_conflict && (
            <p role="alert">
              Termin koliduje z chronioną zmianą. Jej szczegóły są niedostępne.
            </p>
          )}
          {p.data.blackout && (
            <p role="alert">Termin przecina okres zakazu zmian.</p>
          )}
          {p.data.outside_window && (
            <p role="alert">
              Termin nie mieści się w wymaganym oknie serwisowym.
            </p>
          )}
          {!p.data.items.length &&
            !p.data.restricted_conflict &&
            !p.data.blackout &&
            !p.data.outside_window && (
              <p role="status">
                Zapisany termin jest wolny według bieżących danych.
              </p>
            )}
          <p>Planowanie ponownie sprawdzi i zarezerwuje termin atomowo.</p>
        </>
      )}
      <button disabled={p.loading} onClick={p.reload}>
        Sprawdź kolizje ponownie
      </button>
    </section>
  );
}
