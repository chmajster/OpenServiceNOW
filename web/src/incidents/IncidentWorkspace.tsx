import { useCallback, useEffect, useRef, useState } from "react";
import { Link, useLocation, useSearchParams } from "react-router-dom";
import { api } from "../api/client";
import { PageState } from "../components/PageState";
import { RemoteCombobox, type Choice } from "../components/RemoteCombobox";
import {
  auditChanges,
  displayValue,
  fieldLabel,
} from "../components/auditPresentation";
import { IncidentFields, FieldSummary, FormFeedback } from "./FieldsForm";
import {
  draftOf,
  fieldsOf,
  dateLabel,
  optionLabel,
  type Incident,
  type Metadata,
  type HistoryPage,
} from "./contracts";
import {
  useIncidentForm,
  useIncidentQuery,
  useIncidentTitleFocus,
} from "./hooks";

import { CommunicationPanel } from "./CommunicationPanel";
import { ParticipantsPanel } from "./ParticipantsPanel";
import { UnsavedGuard } from "./UnsavedGuard";
import { IncidentSLA } from "../sla/IncidentSLA";
import {
  useWorkspace,
  useWorkspaceRecord,
} from "../workspace/WorkspaceContext";

type EditorProps = {
  record: Incident;
  metadata: Metadata;
  busy: boolean;
  setBusy: (busy: boolean) => void;
  saved: (record: Incident, confirmed?: boolean) => void;
  retain: (tab: string, needed: boolean) => void;
};
export function IncidentWorkspace({
  initial,
  metadata,
}: {
  initial: Incident;
  metadata: Metadata;
}) {
  const [record, setRecord] = useState(initial);
  const workspace = useWorkspace();
  useWorkspaceRecord(
    {
      number: record.number,
      title: record.title,
      state: optionLabel(metadata.states, record.state),
      type: "Incydent",
      assignment: [record.group_name, record.assigned_name]
        .filter(Boolean)
        .join(" · "),
    },
    record.id,
  );
  const [draftTabs, setDraftTabs] = useState<Record<string, boolean>>({});
  const retain = useCallback((tab: string, needed: boolean) => {
    setDraftTabs((current) =>
      current[tab] === needed ? current : { ...current, [tab]: needed },
    );
  }, []);
  const heading = useIncidentTitleFocus(initial.id);
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState<string>();
  const [error, setError] = useState<string>();
  const [params] = useSearchParams();
  const location = useLocation();
  const controller = useRef<AbortController | null>(null);
  const mounted = useRef(true);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
      controller.current?.abort();
    };
  }, []);
  async function refresh() {
    setBusy(true);
    setError(undefined);
    controller.current?.abort();
    const request = new AbortController();
    controller.current = request;
    try {
      const current = await api<Incident>(`/api/v1/incidents/${record.id}`, {
        signal: request.signal,
      });
      if (mounted.current && !request.signal.aborted) {
        setRecord(current);
        setNotice(
          "Odczytano aktualny stan incydentu. Niezapisane propozycje pozostają w formularzach.",
        );
      }
    } catch (cause) {
      if (mounted.current && !request.signal.aborted)
        setError(
          cause instanceof Error
            ? cause.message
            : "Nie udało się odświeżyć incydentu.",
        );
    } finally {
      if (mounted.current) setBusy(false);
    }
  }
  const tab = params.get("tab") ?? "info";
  const tabs = [
    { value: "info", label: "Informacje", available: true },
    {
      value: "edit",
      label: "Edycja",
      available: record.actions.edit || draftTabs.edit,
    },
    {
      value: "assignment",
      label: "Przypisanie",
      available: record.actions.assign || draftTabs.assignment,
    },
    {
      value: "state",
      label: "Stan",
      available: record.actions.transitions.length > 0 || draftTabs.state,
    },
    {
      value: "priority",
      label: "Priorytet",
      available: record.actions.override_priority || draftTabs.priority,
    },
    { value: "communication", label: "Komunikacja i pliki", available: true },
    {
      value: "participants",
      label: "Obserwatorzy i powiązania",
      available: true,
    },
    { value: "history", label: "Historia", available: true },
  ];
  const activeTab =
    tabs.find((t) => t.value === tab && t.available)?.value ?? "info";
  const saved = (updated: Incident, confirmed = true) => {
    setRecord(updated);
    setNotice(
      confirmed
        ? "Zmiana zapisana. Historia jest aktualizowana przez worker."
        : "Odczytano aktualny stan incydentu. Niezapisane propozycje pozostają w formularzach.",
    );
    setError(undefined);
  };
  const props = { record, metadata, busy, setBusy, saved, retain };
  const backSearch =
    (location.state as { listSearch?: string } | null)?.listSearch ??
    workspace?.listURL("/incidents").split("?")[1] ??
    "";
  return (
    <section className="page incident-page">
      <UnsavedGuard dirty={Object.values(draftTabs).some(Boolean)} />
      <div className="page-heading">
        <div>
          <p className="eyebrow">{record.number}</p>
          <h1 ref={heading} tabIndex={-1}>
            {record.title}
          </h1>
          <p>
            <strong>{optionLabel(metadata.states, record.state)}</strong> ·{" "}
            <span className={`incident-priority priority-${record.priority}`}>
              P{record.priority}
            </span>{" "}
            · Wersja {record.version}
          </p>
        </div>
        <div className="action-row">
          <Link
            className="button button--secondary"
            to={`/incidents?${backSearch}`}
          >
            Wróć do listy
          </Link>
          <button
            className="button button--secondary"
            disabled={busy}
            onClick={() => void refresh()}
          >
            Odśwież incydent
          </button>
        </div>
      </div>
      {notice && (
        <p className="notice" role="status">
          {notice}
        </p>
      )}
      {error && (
        <p className="notice notice--error" role="alert">
          {error}
        </p>
      )}
      <nav className="tabs" aria-label="Zakładki incydentu">
        {tabs
          .filter((t) => t.available)
          .map((t) => (
            <Link
              key={t.value}
              className={activeTab === t.value ? "is-active" : ""}
              aria-current={activeTab === t.value ? "page" : undefined}
              to={`?${(() => {
                const next = new URLSearchParams(params);
                next.set("tab", t.value);
                return next;
              })()}`}
            >
              {t.label}
            </Link>
          ))}
      </nav>
      <IncidentSLA id={record.id} version={record.version} />
      <div hidden={activeTab !== "info"} className="panel">
        <h2>Informacje o incydencie</h2>
        <FieldSummary draft={draftOf(record)} metadata={metadata} />
        <dl className="detail-grid">
          <div>
            <dt>Zgłaszający</dt>
            <dd>{record.reporter_name}</dd>
          </div>
          <div>
            <dt>Przypisany użytkownik</dt>
            <dd>{record.assigned_name || "Nieprzypisany"}</dd>
          </div>
          <div>
            <dt>Grupa</dt>
            <dd>{record.group_name || "Bez grupy"}</dd>
          </div>
          <div>
            <dt>Priorytet</dt>
            <dd>
              P{record.priority} (wyliczony P{record.calculated_priority})
            </dd>
          </div>
          {record.priority_override !== null && (
            <div>
              <dt>Uzasadnienie priorytetu</dt>
              <dd>{record.priority_reason}</dd>
            </div>
          )}
          <div>
            <dt>Utworzono</dt>
            <dd>{dateLabel(record.created_at)}</dd>
          </div>
          <div>
            <dt>Zmieniono</dt>
            <dd>{dateLabel(record.updated_at)}</dd>
          </div>
          <div>
            <dt>Rozpoczęto obsługę</dt>
            <dd>{dateLabel(record.started_at)}</dd>
          </div>
          <div>
            <dt>Rozwiązano</dt>
            <dd>{dateLabel(record.resolved_at)}</dd>
          </div>
          <div>
            <dt>Zamknięto</dt>
            <dd>{dateLabel(record.closed_at)}</dd>
          </div>
          <div>
            <dt>Anulowano</dt>
            <dd>{dateLabel(record.cancelled_at)}</dd>
          </div>
        </dl>
        {record.hold_reason && (
          <p className="notice">Powód wstrzymania: {record.hold_reason}</p>
        )}
        {record.resolution_code && (
          <div>
            <h3>
              {optionLabel(metadata.resolution_codes, record.resolution_code)}
            </h3>
            <p className="preserve-lines">{record.resolution_description}</p>
          </div>
        )}
        {record.cancellation_reason && (
          <p className="notice">
            Powód anulowania: {record.cancellation_reason}
          </p>
        )}
      </div>
      {(record.actions.edit || draftTabs.edit) && (
        <div hidden={activeTab !== "edit"} className="panel">
          <EditIncident {...props} />
        </div>
      )}
      {(record.actions.assign || draftTabs.assignment) && (
        <div hidden={activeTab !== "assignment"} className="panel">
          <AssignIncident {...props} />
        </div>
      )}
      {(record.actions.transitions.length > 0 || draftTabs.state) && (
        <div hidden={activeTab !== "state"} className="panel">
          <TransitionIncident {...props} />
        </div>
      )}
      {(record.actions.override_priority || draftTabs.priority) && (
        <div hidden={activeTab !== "priority"} className="panel">
          <PriorityIncident {...props} />
        </div>
      )}
      <div hidden={activeTab !== "communication"} className="panel">
        <CommunicationPanel record={record} retain={retain} />
      </div>
      {activeTab === "participants" && (
        <div className="panel">
          <ParticipantsPanel record={record} />
        </div>
      )}
      {activeTab === "history" && (
        <HistoryPanel record={record} metadata={metadata} />
      )}
    </section>
  );
}
function useRetainedDraft(
  form: { dirty: boolean; conflict?: Incident | null },
  tab: string,
  retain: EditorProps["retain"],
) {
  const needed = form.dirty || form.conflict !== undefined;
  useEffect(() => {
    retain(tab, needed);
  }, [needed, tab, retain]);
}
function BlockedProposal({ allowed }: { allowed: boolean }) {
  return allowed ? null : (
    <p className="notice" role="status">
      Aktualny stan lub uprawnienia nie pozwalają zapisać tej operacji. Twoja
      propozycja pozostaje dostępna do skopiowania.
    </p>
  );
}
function EditIncident({
  record,
  metadata,
  busy,
  setBusy,
  saved,
  retain,
}: EditorProps) {
  const form = useIncidentForm(record, draftOf, saved, setBusy);
  useRetainedDraft(form, "edit", retain);
  return (
    <>
      <h2>Edycja incydentu</h2>
      <BlockedProposal allowed={record.actions.edit} />
      <form
        onSubmit={(e) =>
          void form.submit(e, `/api/v1/incidents/${record.id}`, "PUT", () => {
            if (!record.actions.edit)
              throw new Error("Zapis edycji nie jest obecnie dozwolony.");
            return fieldsOf(form.draft);
          })
        }
      >
        <IncidentFields
          draft={form.draft}
          change={form.setDraft}
          metadata={metadata}
          disabled={busy}
        />
        <FormFeedback
          form={form}
          busy={busy}
          current={(r) => (
            <FieldSummary draft={draftOf(r)} metadata={metadata} />
          )}
          proposal={<FieldSummary draft={form.draft} metadata={metadata} />}
        />
        <button
          className="button button--primary"
          disabled={busy || !record.actions.edit || form.conflict !== undefined}
        >
          Zapisz dane incydentu
        </button>
      </form>
    </>
  );
}
type AssignmentDraft = { group: Choice | null; user: Choice | null };
const assignmentOf = (r: Incident): AssignmentDraft => ({
  group: r.assignment.assignment_group_id
    ? { id: r.assignment.assignment_group_id, label: r.group_name }
    : null,
  user: r.assignment.assigned_to
    ? { id: r.assignment.assigned_to, label: r.assigned_name }
    : null,
});
function AssignmentSummary({ draft }: { draft: AssignmentDraft }) {
  return (
    <dl className="detail-grid">
      <div>
        <dt>Grupa</dt>
        <dd>{draft.group?.label ?? "Bez grupy"}</dd>
      </div>
      <div>
        <dt>Użytkownik</dt>
        <dd>{draft.user?.label ?? "Nieprzypisany"}</dd>
      </div>
    </dl>
  );
}
function AssignIncident({ record, busy, setBusy, saved, retain }: EditorProps) {
  const form = useIncidentForm(record, assignmentOf, saved, setBusy);
  useRetainedDraft(form, "assignment", retain);
  const endpoint = `/api/v1/incidents/lookups/users?purpose=assignment${form.draft.group ? `&group_id=${form.draft.group.id}` : ""}`;
  return (
    <>
      <h2>Przypisanie incydentu</h2>
      <BlockedProposal allowed={record.actions.assign} />
      <p>
        Użytkownik musi mieć aktywne konto i członkostwo w organizacji. W
        wybranej grupie wybierzesz jej członków.
      </p>
      <form
        onSubmit={(e) =>
          void form.submit(
            e,
            `/api/v1/incidents/${record.id}/assignment`,
            "PUT",
            () => {
              if (!record.actions.assign)
                throw new Error(
                  "Zapis przypisania nie jest obecnie dozwolony.",
                );
              return {
                assigned_to: form.draft.user?.id ?? null,
                assignment_group_id: form.draft.group?.id ?? null,
              };
            },
          )
        }
      >
        <fieldset className="form-fields" disabled={busy}>
          <RemoteCombobox
            kind="groups"
            label="Grupa obsługi"
            endpoint="/api/v1/incidents/lookups/groups"
            disabled={busy}
            value={form.draft.group}
            onChange={(group) => form.setDraft({ group, user: null })}
          />
          <RemoteCombobox
            kind="users"
            label="Osoba przypisana"
            endpoint={endpoint}
            disabled={busy}
            value={form.draft.user}
            onChange={(user) => form.setDraft({ ...form.draft, user })}
          />
        </fieldset>
        <FormFeedback
          form={form}
          busy={busy}
          current={(r) => <AssignmentSummary draft={assignmentOf(r)} />}
          proposal={<AssignmentSummary draft={form.draft} />}
        />
        <button
          className="button button--primary"
          disabled={
            busy || !record.actions.assign || form.conflict !== undefined
          }
        >
          Zapisz przypisanie
        </button>
      </form>
    </>
  );
}
type TransitionDraft = {
  state: string;
  reason: string;
  resolution_code: string;
  resolution_description: string;
};
const transitionOf = (r: Incident): TransitionDraft => ({
  state: r.actions.transitions[0] ?? "",
  reason: "",
  resolution_code: "FIXED",
  resolution_description: "",
});
function TransitionIncident({
  record,
  metadata,
  busy,
  setBusy,
  saved,
  retain,
}: EditorProps) {
  const form = useIncidentForm(record, transitionOf, saved, setBusy);
  useRetainedDraft(form, "state", retain);
  const draft = form.draft;
  const reasonRequired =
    draft.state === "ON_HOLD" ||
    draft.state === "CANCELLED" ||
    (draft.state === "IN_PROGRESS" &&
      (record.state === "RESOLVED" || record.state === "CLOSED"));
  const resolved = draft.state === "RESOLVED";
  return (
    <>
      <h2>Zmiana stanu</h2>
      <BlockedProposal
        allowed={record.actions.transitions.includes(form.draft.state)}
      />
      <p>
        Backend sprawdza dozwolone przejścia. Rozpoczęcie obsługi i rozwiązanie
        wymagają aktywnego przypisania.
      </p>
      <form
        onSubmit={(e) =>
          void form.submit(
            e,
            `/api/v1/incidents/${record.id}/transition`,
            "POST",
            () => {
              if (!record.actions.transitions.includes(draft.state))
                throw new Error(
                  "Wybierz dozwolony stan dla aktualnego incydentu.",
                );
              if (
                reasonRequired &&
                (!draft.reason.trim() ||
                  Array.from(draft.reason.trim()).length > 1000)
              )
                throw new Error("Powód wymaga 1–1000 znaków.");
              if (
                resolved &&
                (!draft.resolution_description.trim() ||
                  Array.from(draft.resolution_description.trim()).length >
                    20000)
              )
                throw new Error("Opis rozwiązania wymaga 1–20000 znaków.");
              return {
                state: draft.state,
                reason: reasonRequired ? draft.reason.trim() : "",
                resolution_code: resolved ? draft.resolution_code : "",
                resolution_description: resolved
                  ? draft.resolution_description.trim()
                  : "",
              };
            },
          )
        }
      >
        <fieldset className="form-fields" disabled={busy}>
          <label>
            Nowy stan
            <select
              aria-label="Nowy stan"
              value={draft.state}
              onChange={(e) =>
                form.setDraft({
                  ...draft,
                  state: e.target.value,
                  reason: "",
                  resolution_description: "",
                })
              }
            >
              {!record.actions.transitions.includes(draft.state) && (
                <option value={draft.state}>
                  {optionLabel(metadata.states, draft.state)} (obecnie
                  niedozwolony)
                </option>
              )}
              {record.actions.transitions.map((state) => (
                <option key={state} value={state}>
                  {optionLabel(metadata.states, state)}
                </option>
              ))}
            </select>
          </label>
          {reasonRequired && (
            <label>
              Powód zmiany
              <textarea
                aria-label="Powód zmiany"
                required
                rows={3}
                value={draft.reason}
                onChange={(e) =>
                  form.setDraft({ ...draft, reason: e.target.value })
                }
              />
            </label>
          )}
          {resolved && (
            <>
              <label>
                Kod rozwiązania
                <select
                  aria-label="Kod rozwiązania"
                  value={draft.resolution_code}
                  onChange={(e) =>
                    form.setDraft({ ...draft, resolution_code: e.target.value })
                  }
                >
                  {metadata.resolution_codes.map((o) => (
                    <option key={o.value} value={o.value}>
                      {o.label}
                    </option>
                  ))}
                </select>
              </label>
              <label>
                Opis rozwiązania
                <textarea
                  aria-label="Opis rozwiązania"
                  required
                  rows={4}
                  value={draft.resolution_description}
                  onChange={(e) =>
                    form.setDraft({
                      ...draft,
                      resolution_description: e.target.value,
                    })
                  }
                />
              </label>
            </>
          )}
        </fieldset>
        <FormFeedback
          form={form}
          busy={busy}
          current={(r) => (
            <p>
              Stan: {optionLabel(metadata.states, r.state)}; wersja {r.version}
            </p>
          )}
          proposal={
            <div>
              <p>Stan: {optionLabel(metadata.states, draft.state)}</p>
              {draft.reason && <p>{draft.reason}</p>}
              {resolved && (
                <p>
                  {optionLabel(
                    metadata.resolution_codes,
                    draft.resolution_code,
                  )}
                  : {draft.resolution_description}
                </p>
              )}
            </div>
          }
        />
        <button
          className="button button--primary"
          disabled={
            busy ||
            !record.actions.transitions.includes(form.draft.state) ||
            form.conflict !== undefined
          }
        >
          Zmień stan
        </button>
      </form>
    </>
  );
}
type PriorityDraft = { priority: string; reason: string };
const priorityOf = (r: Incident): PriorityDraft => ({
  priority: r.priority_override !== null ? String(r.priority) : "auto",
  reason: r.priority_reason,
});
function PriorityIncident({
  record,
  busy,
  setBusy,
  saved,
  retain,
}: EditorProps) {
  const form = useIncidentForm(record, priorityOf, saved, setBusy);
  useRetainedDraft(form, "priority", retain);
  return (
    <>
      <h2>Priorytet incydentu</h2>
      <BlockedProposal allowed={record.actions.override_priority} />
      <p>
        Priorytet wyliczony: P{record.calculated_priority}. Nadpisanie wymaga
        osobnego uprawnienia, uzasadnienia i jest audytowane.
      </p>
      <form
        onSubmit={(e) =>
          void form.submit(
            e,
            `/api/v1/incidents/${record.id}/priority`,
            "PUT",
            () => {
              if (!record.actions.override_priority)
                throw new Error("Zapis priorytetu nie jest obecnie dozwolony.");
              const priority =
                form.draft.priority === "auto"
                  ? null
                  : Number(form.draft.priority);
              if (
                priority !== null &&
                priority !== record.calculated_priority &&
                (!form.draft.reason.trim() ||
                  Array.from(form.draft.reason.trim()).length > 1000)
              )
                throw new Error("Uzasadnienie wymaga 1–1000 znaków.");
              return {
                priority,
                reason: priority !== null ? form.draft.reason.trim() : "",
              };
            },
          )
        }
      >
        <fieldset className="form-fields" disabled={busy}>
          <label>
            Priorytet docelowy
            <select
              aria-label="Priorytet docelowy"
              value={form.draft.priority}
              onChange={(e) =>
                form.setDraft({ ...form.draft, priority: e.target.value })
              }
            >
              <option value="auto">
                Z macierzy — P{record.calculated_priority}
              </option>
              {[1, 2, 3, 4, 5].map((n) => (
                <option key={n} value={n}>
                  P{n}
                </option>
              ))}
            </select>
          </label>
          {form.draft.priority !== "auto" && (
            <label>
              Uzasadnienie priorytetu
              <textarea
                aria-label="Uzasadnienie priorytetu"
                rows={3}
                value={form.draft.reason}
                onChange={(e) =>
                  form.setDraft({ ...form.draft, reason: e.target.value })
                }
              />
            </label>
          )}
        </fieldset>
        <FormFeedback
          form={form}
          busy={busy}
          current={(r) => (
            <p>
              P{r.priority}: {r.priority_reason || "Priorytet z macierzy"}
            </p>
          )}
          proposal={
            <p>
              {form.draft.priority === "auto"
                ? "Priorytet z macierzy"
                : `P${form.draft.priority}`}
              : {form.draft.reason}
            </p>
          }
        />
        <button
          className="button button--primary"
          disabled={
            busy ||
            !record.actions.override_priority ||
            form.conflict !== undefined
          }
        >
          Zapisz priorytet
        </button>
      </form>
    </>
  );
}
function HistoryPanel({
  record,
  metadata,
}: {
  record: Incident;
  metadata: Metadata;
}) {
  const [params, setParams] = useSearchParams();
  const before = params.get("history_before_event_id");
  const history = useIncidentQuery<HistoryPage>(
    `/api/v1/incidents/${record.id}/history?limit=20${before ? `&before_event_id=${before}` : ""}`,
  );
  function value(field: string, item: unknown) {
    if (item === undefined) return "—";
    if (typeof item === "string") {
      if (field === "state") return optionLabel(metadata.states, item);
      if (field === "category") return optionLabel(metadata.categories, item);
      if (field === "subcategory")
        return optionLabel(
          metadata.categories.flatMap((c) => c.subcategories),
          item,
        );
      if (field === "resolution_code")
        return optionLabel(metadata.resolution_codes, item);
      if (field.endsWith("_at")) return dateLabel(item);
    }
    if (field === "priority" || field === "priority_override")
      return item === null ? "Z macierzy" : `P${item}`;
    return displayValue(item);
  }
  return (
    <div className="panel">
      <div className="action-row">
        <h2>Historia incydentu</h2>
        <button
          className="button button--secondary"
          disabled={history.loading}
          onClick={history.reload}
        >
          Odśwież historię
        </button>
      </div>
      {history.loading ? (
        <PageState title="Ładowanie historii…" />
      ) : history.error ? (
        <PageState
          title="Nie udało się odczytać historii"
          detail={history.error}
          tone="error"
        />
      ) : (
        <>
          {history.data?.pending && (
            <p className="notice" role="status">
              Historia oczekuje na przetworzenie zdarzeń przez worker. Zapis
              incydentu jest trwały. Odśwież historię za chwilę.
            </p>
          )}
          {!history.data?.entries.length && (
            <PageState title="Historia nie zawiera jeszcze zdarzeń" />
          )}
          <ol className="incident-timeline" aria-label="Historia zdarzeń">
            {history.data?.entries.map((entry) => (
              <li key={entry.id}>
                <h3>{entry.summary}</h3>
                <p className="muted">
                  {dateLabel(entry.event_time)} ·{" "}
                  {entry.actor_name ||
                    "Użytkownik " + (entry.actor_user_id ?? "—")}{" "}
                  · {entry.type} · Wersja {entry.data.incident_version}
                </p>
                {entry.data.reason && <p>Uzasadnienie: {entry.data.reason}</p>}
                {entry.data.body && (
                  <p className="preserve-lines">{entry.data.body}</p>
                )}
                {entry.data.file_name && <p>Plik: {entry.data.file_name}</p>}
                <dl className="incident-changes">
                  {auditChanges(entry.data.before, entry.data.after)
                    .filter(
                      (change) =>
                        ![
                          "id",
                          "number",
                          "reporter_id",
                          "created_at",
                          "updated_at",
                          "version",
                        ].includes(change.field),
                    )
                    .map((change) => (
                      <div key={change.field}>
                        <dt>{fieldLabel(change.field)}</dt>
                        <dd>
                          <span>{value(change.field, change.before)}</span>
                          <span aria-label="zmieniono na"> → </span>
                          <strong>{value(change.field, change.after)}</strong>
                        </dd>
                      </div>
                    ))}
                </dl>
                <details>
                  <summary>Szczegóły techniczne zdarzenia</summary>
                  <p>Identyfikator aktora: {entry.actor_user_id ?? "—"}</p>
                  <pre>{JSON.stringify(entry.data, null, 2)}</pre>
                </details>
              </li>
            ))}
          </ol>
        </>
      )}
      <div className="action-row">
        {before && (
          <button
            className="button button--secondary"
            disabled={history.loading}
            onClick={() => {
              const next = new URLSearchParams(params);
              next.delete("history_before_event_id");
              setParams(next);
            }}
          >
            Najnowsze zdarzenia
          </button>
        )}
        {history.data?.next_before_event_id && (
          <button
            className="button button--secondary"
            disabled={history.loading}
            onClick={() => {
              const next = new URLSearchParams(params);
              next.set(
                "history_before_event_id",
                history.data!.next_before_event_id!,
              );
              setParams(next);
            }}
          >
            Starsze zdarzenia
          </button>
        )}
      </div>
    </div>
  );
}
