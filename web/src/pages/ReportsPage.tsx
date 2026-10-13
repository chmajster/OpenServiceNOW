import { useEffect, useState } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { useAuth } from "../auth/AuthContext";
import { PageState } from "../components/PageState";

import { useIncidentQuery } from "../incidents/hooks";
import { RecordTable, type RecordColumn } from "../recordlists/RecordTable";
import { columnsFrom } from "../recordlists/contracts";
import {
  type Reference,
  referenceState,
  referenceStateLabel,
} from "../workspace/references";
type Bucket = {
  value: string;
  label: string;
  count: string;
  url: string;
  record?: Reference;
};
type Report = {
  key: string;
  domain: string;
  label: string;
  definition: string;
  time_scope: "snapshot" | "range";
  eligible: string;
  total: string;
  items: Bucket[];
};
type Reports = {
  items: Report[];
  period: { from: string; to: string };
  calculated_at: string;
};
const domainLabels: Record<string, string> = {
  incident: "Incydenty",
  request: "Wnioski",
  problem: "Problemy",
  change: "Zmiany",
};
const readable = [
  "incident.read",
  "incident.read_own",
  "request.read_own",
  "request.fulfill",
  "request.manage",
  "problem.read",
  "problem.read_own",
  "change.read",
  "change.read_own",
];
function dateOnly(stamp: string) {
  const time = Date.parse(stamp);
  return Number.isFinite(time) ? new Date(time).toISOString().slice(0, 10) : "";
}
function defaultRange() {
  const now = new Date(),
    end = new Date(
      Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate() + 1),
    );
  return {
    from: new Date(end.getTime() - 30 * 86400000).toISOString(),
    to: end.toISOString(),
  };
}
function validRange(from: string, to: string) {
  const start = Date.parse(from),
    end = Date.parse(to);
  return (
    Number.isFinite(start) &&
    Number.isFinite(end) &&
    end > start &&
    end - start <= 366 * 86400000
  );
}
export function ReportsPage() {
  const { hasPermission } = useAuth(),
    [params, setParams] = useSearchParams(),
    [defaults] = useState(defaultRange);
  const from = params.get("from") ?? defaults.from,
    to = params.get("to") ?? defaults.to,
    valid = validRange(from, to);
  const [start, setStart] = useState(dateOnly(from)),
    [end, setEnd] = useState(dateOnly(to)),
    [error, setError] = useState("");
  useEffect(() => {
    setStart(dateOnly(from));
    setEnd(dateOnly(to));
  }, [from, to]);
  const path = valid
    ? `/api/v1/workspace/reports?${new URLSearchParams({ from, to })}`
    : null;
  const q = useIncidentQuery<Reports>(path),
    domain = params.get("domain") ?? "all";
  if (!readable.some(hasPermission))
    return <PageState title="Brak dostępu do raportów" />;
  const reports =
    q.data?.items.filter((r) => domain === "all" || r.domain === domain) ?? [];
  return (
    <section className="page">
      <div className="page-heading">
        <div>
          <p className="eyebrow">Dane operacyjne · bieżące uprawnienia</p>
          <h1>Raporty operacyjne</h1>
          <p className="muted">
            Aktywne sprawy i backlog pokazują stan w chwili obliczenia. Zakres
            dat dotyczy rankingu incydentów oraz planowanych i zakończonych
            zmian.
          </p>
        </div>
        <button
          className="button button--secondary"
          disabled={q.loading || !valid}
          onClick={q.reload}
        >
          Odśwież raporty
        </button>
      </div>
      <form
        className="filter-bar"
        onSubmit={(e) => {
          e.preventDefault();
          const f = `${start}T00:00:00Z`,
            t = `${end}T00:00:00Z`;
          if (!validRange(f, t)) {
            setError("Wybierz dodatni przedział o długości do 366 dni.");
            return;
          }
          setError("");
          const n = new URLSearchParams(params);
          n.set("from", f);
          n.set("to", t);
          setParams(n);
        }}
      >
        <label>
          Od dnia (UTC)
          <input
            type="date"
            required
            value={start}
            onChange={(e) => setStart(e.target.value)}
          />
        </label>
        <label>
          Do dnia — bez tego dnia (UTC)
          <input
            type="date"
            required
            value={end}
            onChange={(e) => setEnd(e.target.value)}
          />
        </label>
        <button className="button">Zastosuj zakres dat</button>
        <label>
          Proces
          <select
            value={domain}
            onChange={(e) => {
              const n = new URLSearchParams(params);
              n.set("domain", e.target.value);
              setParams(n);
            }}
          >
            <option value="all">Wszystkie dostępne</option>
            {Object.entries(domainLabels).map(([value, label]) => (
              <option key={value} value={value}>
                {label}
              </option>
            ))}
          </select>
        </label>
      </form>
      {(error || !valid) && (
        <p role="alert">
          {error ||
            "Nieprawidłowy zakres dat w adresie. Wybierz od i do, maksymalnie 366 dni."}
        </p>
      )}
      {valid &&
        (q.loading ? (
          <PageState title="Obliczanie raportów…" />
        ) : q.error ? (
          <PageState
            title="Nie udało się pobrać raportów"
            detail={q.error}
            tone="error"
            action={<button onClick={q.reload}>Spróbuj ponownie</button>}
          />
        ) : (
          <>
            <p className="muted">
              Obliczono{" "}
              {q.data && new Date(q.data.calculated_at).toLocaleString("pl-PL")}{" "}
              · zakres dat {dateOnly(from)} – {dateOnly(to)} (UTC, koniec
              wyłączony). Każdy licznik prowadzi do ponownie sprawdzanej listy
              źródłowej.
            </p>
            {!reports.length ? (
              <PageState title="Brak raportów dla wybranego procesu" />
            ) : (
              <div className="operational-reports">
                {reports.map((r) => (
                  <article className="panel" key={`${r.domain}:${r.key}`}>
                    <h2>{r.label}</h2>
                    <p>{r.definition}</p>
                    <small className="muted">
                      {r.time_scope === "snapshot"
                        ? "Stan w chwili obliczenia, bez ograniczenia datą utworzenia"
                        : "Wybrany zakres dat UTC"}{" "}
                      · Suma: {r.total}
                    </small>
                    {r.eligible === "0" ? (
                      <p role="status">
                        Brak dostępnych rekordów spełniających definicję.
                      </p>
                    ) : !r.items.length ? (
                      <p role="status">
                        0 — brak dostępnych powiązań w tym zakresie.
                      </p>
                    ) : (
                      <div className="table-scroll">
                        <table className="record-table">
                          <thead>
                            <tr>
                              <th scope="col">
                                {r.key === "problem_incidents"
                                  ? "Problem"
                                  : "Grupa wyników"}
                              </th>
                              <th scope="col">Liczba</th>
                            </tr>
                          </thead>
                          <tbody>
                            {r.items.map((b) => (
                              <tr key={b.value}>
                                <td>
                                  {b.record ? (
                                    <>
                                      <Link to={b.record.url}>{b.label}</Link> ·{" "}
                                      <Link to={b.url}>
                                        Powiązane incydenty
                                      </Link>
                                    </>
                                  ) : (
                                    <Link to={b.url}>
                                      {[
                                        "active_state",
                                        "planned_changes",
                                        "closed_changes",
                                      ].includes(r.key)
                                        ? referenceStateLabel(r.domain, b.label)
                                        : b.label}
                                    </Link>
                                  )}
                                </td>
                                <td>
                                  <Link
                                    to={b.url}
                                    aria-label={`${b.label}: ${b.count}`}
                                  >
                                    {b.count}
                                  </Link>
                                </td>
                              </tr>
                            ))}
                          </tbody>
                        </table>
                      </div>
                    )}
                  </article>
                ))}
              </div>
            )}
          </>
        ))}
    </section>
  );
}
const bucketLabels: Record<string, string> = {
  "0_7": "Mniej niż 7 dni",
  "7_30": "Od 7 do mniej niż 30 dni",
  "30_plus": "Co najmniej 30 dni",
  unassigned: "Bez grupy",
};
const reportRoots: Record<string, string> = {
  incident: "incidents",
  request: "requests",
  problem: "problems",
  change: "changes",
};
const reportLabels: Record<string, string> = {
  active_state: "Aktywne sprawy według stanu",
  active_group: "Aktywne sprawy według grupy",
  backlog_age: "Wiek backlogu",
  sla_risk: "Zagrożone SLA incydentów",
  problem_incidents: "Incydenty powiązane z problemem",
  planned_changes: "Zaplanowane zmiany",
  closed_changes: "Zakończone zmiany",
};
export function ReportSourcesPage() {
  const { hasPermission } = useAuth(),
    [params, setParams] = useSearchParams(),
    qparams = new URLSearchParams(params);
  qparams.delete("columns");
  if (!qparams.has("limit")) qparams.set("limit", "25");
  const domain = params.get("domain") ?? "",
    report = params.get("report") ?? "",
    root = reportRoots[domain];
  const q = useIncidentQuery<{
    items: Reference[];
    total: string;
    next_before_id?: string;
    calculated_at: string;
  }>(`/api/v1/workspace/report-records?${qparams}`);
  if (!readable.some(hasPermission))
    return <PageState title="Brak dostępu do źródeł raportów" />;
  const columns: RecordColumn<Reference>[] = [
    {
      key: "number",
      label: "Numer",
      width: 160,
      render: (r) => <Link to={r.url}>{r.number}</Link>,
    },
    {
      key: "title",
      label: "Opis skrócony",
      width: 350,
      render: (r) => r.title,
    },
    { key: "state", label: "Bieżący stan", width: 180, render: referenceState },
  ];
  function page(before?: string) {
    const n = new URLSearchParams(params);
    before ? n.set("before_id", before) : n.delete("before_id");
    setParams(n);
  }
  const back = new URLSearchParams();
  for (const key of ["from", "to"])
    if (params.has(key)) back.set(key, params.get(key)!);
  back.set("domain", domain);
  return (
    <section className="page">
      <div className="page-heading">
        <div>
          <p className="eyebrow">
            Rekordy źródłowe · {domainLabels[domain] ?? "Nieznany proces"}
          </p>
          <h1>{reportLabels[report] ?? "Źródła raportu"}</h1>
          <p className="muted">
            Wynik jest ponownie obliczany z aktualnymi uprawnieniami i stanami
            rekordów; liczba może zmienić się od otwarcia raportu. Granice wieku
            backlogu zachowują chwilę jego obliczenia.
          </p>
        </div>
        <Link className="button button--secondary" to={`/reports?${back}`}>
          Wróć do raportów
        </Link>
        <button
          className="button button--secondary"
          disabled={q.loading}
          onClick={q.reload}
        >
          Odśwież źródła
        </button>
      </div>
      <p>
        Filtr: {domainLabels[domain] ?? domain} ·{" "}
        {params.get("bucket")
          ? (bucketLabels[params.get("bucket")!] ??
            referenceStateLabel(domain, params.get("bucket")!))
          : "Wszystkie spełniające definicję"}
        {params.has("problem_id") && ` · problem ${params.get("problem_id")}`} ·
        od {params.get("from")} do {params.get("to")} (koniec wyłączony).
      </p>
      {q.loading ? (
        <PageState title="Odczyt rekordów źródłowych…" />
      ) : q.error ? (
        <PageState
          title="Nie udało się pobrać źródeł"
          detail={q.error}
          tone="error"
          action={<button onClick={q.reload}>Spróbuj ponownie</button>}
        />
      ) : (
        <>
          <p role="status">Dostępnych rekordów: {q.data?.total ?? "0"}</p>
          {q.data?.items.length && root ? (
            <RecordTable
              records={q.data.items}
              columns={columns}
              selected={columnsFrom(params.get("columns"), columns)}
              url={(r) => r.url}
              apiRoot={`/api/v1/${root}`}
              stateLabel={(state) => referenceStateLabel(domain, state)}
            />
          ) : (
            <PageState title="Brak dostępnych rekordów dla tej definicji" />
          )}
          <div className="action-row">
            {params.has("before_id") && (
              <button
                className="button button--secondary"
                onClick={() => page()}
              >
                Pierwsza strona
              </button>
            )}
            {q.data?.next_before_id && (
              <button
                className="button"
                onClick={() => page(q.data!.next_before_id)}
              >
                Następna strona
              </button>
            )}
          </div>
        </>
      )}
    </section>
  );
}
