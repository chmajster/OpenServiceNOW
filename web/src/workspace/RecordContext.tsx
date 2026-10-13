import { Link } from "react-router-dom";
import { useIncidentQuery } from "../incidents/hooks";
import { type Reference, referenceType, referenceState } from "./references";
type Section = {
  key: string;
  label: string;
  definition: string;
  items: Reference[];
  more_url: string;
  has_more: boolean;
};
export function RecordContext({ recordKey }: { recordKey: string }) {
  const q = useIncidentQuery<{ record: Reference; sections: Section[] }>(
    `/api/v1/workspace/context${recordKey}`,
  );
  if (q.loading) return <p role="status">Odczyt powiązań…</p>;
  if (q.error)
    return (
      <div role="alert">
        <p>{q.error}</p>
        <button className="button button--secondary" onClick={q.reload}>
          Ponów odczyt kontekstu
        </button>
      </div>
    );
  return (
    <div className="record-context-relations">
      <button
        className="button button--small button--secondary"
        onClick={q.reload}
      >
        Odśwież kontekst
      </button>
      {q.data?.sections.map((s) => (
        <section key={s.key}>
          <h3>{s.label}</h3>
          <p className="muted">{s.definition}</p>
          {s.items.length ? (
            <ul>
              {s.items.map((r) => (
                <li key={`${r.type}:${r.id}`}>
                  <Link to={r.url}>
                    {r.number} · {r.title}
                  </Link>
                  <small>
                    {referenceType(r)} · {referenceState(r)}
                  </small>
                </li>
              ))}
            </ul>
          ) : (
            <p>Brak dostępnych powiązań.</p>
          )}
          {s.has_more && (
            <Link to={s.more_url}>Wszystkie dostępne powiązania</Link>
          )}
        </section>
      ))}
    </div>
  );
}
