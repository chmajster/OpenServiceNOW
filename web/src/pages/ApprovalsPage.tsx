import { Link, useParams } from "react-router-dom";
import { useRemotePage } from "../components/useRemotePage";
import { LoadState } from "../requests/LoadState";
import { useAuth } from "../auth/AuthContext";
import { ProcessDetails } from "../workflow/ProcessView";
import type { Execution } from "../workflow/contracts";
export function ApprovalsPage() {
  const { executionID } = useParams();
  return executionID ? (
    <ProcessDetails key={executionID} id={executionID} />
  ) : (
    <Inbox />
  );
}
function Inbox() {
  const { hasPermission } = useAuth();
  const p = useRemotePage<Execution>(
    "/api/v1/workflow/approvals?limit=25",
    "items",
    "before_id",
    hasPermission("workflow.approve"),
  );
  return (
    <>
      <h1>Moje akceptacje</h1>
      <p>
        Widoczne są Twoje oczekujące decyzje, z aktualnym dostępem i
        nieprzekroczonym terminem.
      </p>
      {!hasPermission("workflow.approve") ? (
        <LoadState error="Brak uprawnień do podejmowania decyzji." />
      ) : (
        <>
          <button
            className="button button--secondary"
            onClick={() => void p.retry()}
            disabled={p.loading}
          >
            Odśwież akceptacje
          </button>
          <LoadState loading={p.loading} error={p.error} retry={p.retry} />
          {!p.loading && !p.error && !p.items.length ? (
            <p role="status">Brak oczekujących akceptacji.</p>
          ) : null}
          <div className="table-scroll">
            <table>
              <thead>
                <tr>
                  <th>Rekord</th>
                  <th>Proces</th>
                  <th>Krok</th>
                </tr>
              </thead>
              <tbody>
                {p.items.map((x) => (
                  <tr key={x.id}>
                    <td>
                      <Link to={`/approvals/${x.id}`}>
                        {x.change_number || x.request_number}
                      </Link>
                    </td>
                    <td>
                      {x.name} · wersja {x.definition_version}
                    </td>
                    <td>{x.current_node}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          {p.next ? (
            <button
              className="button button--secondary"
              disabled={p.loading}
              onClick={() => void p.more()}
            >
              Więcej akceptacji
            </button>
          ) : null}
        </>
      )}
    </>
  );
}
