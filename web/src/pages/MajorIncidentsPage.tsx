import { useEffect, useRef, useState } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { api } from "../api/client";
import { useAuth } from "../auth/AuthContext";
import { useCMDBLocale } from "../cmdb/messages";
import { formatDateTime } from "../metadata/format";
import { CoordinationActionEditor } from "../incidents/CoordinationActionEditor";
import { MajorActionList } from "../incidents/MajorPanel";
import { majorMessages } from "../incidents/majorMessages";
import type { MajorAction, MajorCase } from "../incidents/majorContracts";

export function MajorIncidentsPage() {
  const locale = useCMDBLocale(),
    t = majorMessages[locale],
    { hasPermission } = useAuth(),
    [params, setParams] = useSearchParams();
  const actions = params.get("view") === "actions";
  const canMajor =
    hasPermission("incident.major.read") ||
    hasPermission("incident.major.manage");
  const [items, setItems] = useState<MajorCase[]>([]),
    [tasks, setTasks] = useState<MajorAction[]>([]),
    [next, setNext] = useState("0");
  const [busy, setBusy] = useState(false),
    [error, setError] = useState<string>(),
    [filter, setFilter] = useState("");
  const controller = useRef<AbortController | null>(null);
  async function load(after = "0") {
    controller.current?.abort();
    const request = new AbortController();
    controller.current = request;
    setBusy(true);
    setError(undefined);
    try {
      if (actions) {
        const page = await api<{ items: MajorAction[]; next_after_id: string }>(
          `/api/v1/incidents/major/actions?limit=25&after_id=${after === "0" ? "" : after}`,
          { signal: request.signal },
        );
        if (!request.signal.aborted) {
          setTasks((old) =>
            after === "0" ? page.items : [...old, ...page.items],
          );
          setNext(page.next_after_id);
        }
      } else {
        const page = await api<{ items: MajorCase[]; next_after_id: string }>(
          `/api/v1/incidents/major?limit=25&view=${filter}&after_id=${after === "0" ? "" : after}`,
          { signal: request.signal },
        );
        if (!request.signal.aborted) {
          setItems((old) =>
            after === "0" ? page.items : [...old, ...page.items],
          );
          setNext(page.next_after_id);
        }
      }
    } catch (cause) {
      if (!request.signal.aborted)
        setError(cause instanceof Error ? cause.message : t.error);
    } finally {
      if (!request.signal.aborted) setBusy(false);
    }
  }
  useEffect(() => {
    void load();
    return () => controller.current?.abort();
  }, [actions, filter]);
  async function change(task: MajorAction, status: string) {
    if (busy) return;
    setBusy(true);
    setError(undefined);
    try {
      await api(`/api/v1/incidents/major/actions/${task.id}`, {
        method: "PUT",
        body: JSON.stringify({ version: task.version, status }),
      });
      await load();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : t.error);
    } finally {
      setBusy(false);
    }
  }
  if (!actions && !canMajor)
    return (
      <section className="module08-page">
        <h1>{t.title}</h1>
        <p>{t.readOnly}</p>
        <Link to="?view=actions">{t.actions}</Link>
      </section>
    );
  return (
    <section className="module08-page">
      <header className="section-header">
        <h1>{actions ? t.actions : t.title}</h1>
        <button
          className="button button--secondary"
          disabled={busy}
          onClick={() => void load()}
        >
          {t.refresh}
        </button>
      </header>
      <nav className="tabs" aria-label={t.coordination}>
        {canMajor && (
          <button
            className="button button--secondary"
            aria-current={!actions ? "page" : undefined}
            onClick={() => setParams({})}
          >
            {t.title}
          </button>
        )}
        <button
          className="button button--secondary"
          aria-current={actions ? "page" : undefined}
          onClick={() => setParams({ view: "actions" })}
        >
          {t.actions}
        </button>
        <Link to="/workspace">{t.workspace}</Link>
      </nav>
      {error && (
        <p role="alert" className="notice notice--error">
          {error}
        </p>
      )}
      {busy && <p role="status">{t.loading}</p>}
      {actions && params.get("action_id") && (
        <CoordinationActionEditor
          key={params.get("action_id")}
          id={params.get("action_id")!}
        />
      )}
      {actions ? (
        <MajorActionList
          items={tasks}
          busy={busy}
          change={(task, state) => void change(task, state)}
        />
      ) : (
        <>
          <label>
            {t.status}
            <select value={filter} onChange={(e) => setFilter(e.target.value)}>
              <option value="">{t.title}</option>
              <option value="candidates">{t.stateLabels.CANDIDATE}</option>
              <option value="active">{t.stateLabels.ACTIVE}</option>
            </select>
          </label>
          {!items.length && !busy && <p>{t.empty}</p>}
          <div className="table-scroll">
            <table>
              <thead>
                <tr>
                  <th>{t.title}</th>
                  <th>{t.status}</th>
                  <th>{t.lastUpdate}</th>
                  <th>{t.nextUpdate}</th>
                  <th>{t.review}</th>
                  <th>{t.unassigned}</th>
                </tr>
              </thead>
              <tbody>
                {items.map((item) => (
                  <tr key={item.incident_id}>
                    <td>
                      <Link to={`/incidents/${item.incident_id}?tab=major`}>
                        {item.number} · {item.title}
                      </Link>
                    </td>
                    <td>{t.stateLabels[item.status]}</td>
                    <td>
                      {item.last_update_at
                        ? formatDateTime(item.last_update_at, locale)
                        : "—"}
                    </td>
                    <td>
                      {item.next_update_at
                        ? formatDateTime(item.next_update_at, locale)
                        : "—"}
                    </td>
                    <td>
                      {t.stateLabels[item.review_status ?? ""] ??
                        item.review_status}
                    </td>
                    <td>{item.unassigned_actions ?? "0"}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </>
      )}
      {next && next !== "0" && (
        <button
          className="button button--secondary"
          disabled={busy}
          onClick={() => void load(next)}
        >
          {t.more}
        </button>
      )}
    </section>
  );
}
