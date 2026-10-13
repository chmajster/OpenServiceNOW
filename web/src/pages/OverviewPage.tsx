import { Link } from "react-router-dom";
import { useAuth } from "../auth/AuthContext";
import { useIncidentQuery } from "../incidents/hooks";
import { PageState } from "../components/PageState";
type Summary = {
  metrics: {
    key: string;
    label: string;
    count: string;
    url: string;
    definition: string;
  }[];
  calculated_at: string;
};
export function OverviewPage() {
  const { session } = useAuth();
  const summary = useIncidentQuery<Summary>("/api/v1/workspace/summary");
  return (
    <section className="page">
      <div className="page-heading">
        <div>
          <p className="eyebrow">
            Workspace operatora · {session?.tenant.name}
          </p>
          <h1>Mój obszar pracy</h1>
          <p className="muted">
            Kolejki wymagające uwagi. Wybierz licznik, aby otworzyć listę z tym
            samym zakresem.
          </p>
        </div>
        <button
          className="button button--secondary"
          disabled={summary.loading}
          onClick={summary.reload}
        >
          Odśwież obszar pracy
        </button>
      </div>
      {summary.loading ? (
        <PageState title="Ładowanie kolejek…" />
      ) : summary.error ? (
        <PageState
          title="Nie udało się pobrać kolejek"
          detail={summary.error}
          tone="error"
        />
      ) : summary.data?.metrics.length ? (
        <div className="panel">
          <table className="work-queues">
            <caption>
              Kolejki bieżącej organizacji · stan z{" "}
              {new Date(summary.data.calculated_at).toLocaleString("pl-PL")}
            </caption>
            <thead>
              <tr>
                <th scope="col">Kolejka</th>
                <th scope="col">Liczba</th>
                <th scope="col">Definicja i zakres</th>
              </tr>
            </thead>
            <tbody>
              {summary.data.metrics.map((m) => (
                <tr key={m.key}>
                  <td>
                    <Link to={m.url}>{m.label}</Link>
                  </td>
                  <td>
                    <Link to={m.url} aria-label={`${m.label}: ${m.count}`}>
                      {m.count}
                    </Link>
                  </td>
                  <td>{m.definition}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : (
        <PageState
          title="Brak dostępnych kolejek"
          detail="Administrator może nadać dostęp do procesów. Powiadomienia i konto są dostępne w menu użytkownika."
        />
      )}
    </section>
  );
}
