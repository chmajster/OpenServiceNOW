import { useEffect, useRef, useState } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { api, ApiError, jsonBody } from "../api/client";
import { InventoryChoice, More, PageFeedback } from "../cmdb/components";
import { useRemotePage } from "../components/useRemotePage";
import type { Choice } from "../components/RemoteCombobox";
import { useIncidentQuery } from "../incidents/hooks";
import { useCreation } from "../requests/useCreation";
import { useEditor } from "../requests/useEditor";
import {
  errorText,
  labels,
  type AnalyticalTask,
  type Problem,
  type SourceIncident,
} from "./contracts";

export function ProblemIncidents({
  record,
  onChanged,
}: {
  record: Problem;
  onChanged: () => Promise<void>;
}) {
  const path = `/api/v1/problems/${record.id}/incidents`,
    p = useRemotePage<SourceIncident>(path + "?limit=25", "items", "before_id");
  const [choice, setChoice] = useState<Choice | null>(null),
    [busy, setBusy] = useState(false),
    [error, setError] = useState(""),
    [notice, setNotice] = useState(""),
    [uncertain, setUncertain] = useState(false);
  const selected = useIncidentQuery<SourceIncident>(
    choice ? `/api/v1/problems/source-incidents/${choice.id}` : null,
  );
  const operation = useRef<
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
  async function change(incident: SourceIncident | undefined, remove: boolean) {
    if (busy || (!incident && !operation.current)) return;
    setBusy(true);
    setError("");
    const c = new AbortController();
    controller.current = c;
    try {
      let proposal =
        uncertain && operation.current
          ? operation.current
          : {
              path: `${path}/${incident!.id}`,
              body: jsonBody({
                version: record.version,
                incident_version: incident!.version,
              }),
              method: remove ? "DELETE" : "PUT",
              key: crypto.randomUUID(),
            };
      if (uncertain && operation.current) proposal = operation.current;
      else operation.current = proposal;
      await api(proposal.path, {
        method: proposal.method,
        body: proposal.body,
        headers: { "Idempotency-Key": proposal.key },
        signal: c.signal,
      });
      if (!alive.current) return;
      setUncertain(false);
      operation.current = undefined;
      setChoice(null);
      setNotice("Zapisano powiązanie. Stan incydentu pozostaje bez zmian.");
      p.reload();
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
    <section>
      <h3>Powiązane incydenty</h3>
      <button
        disabled={busy || uncertain}
        onClick={() => {
          p.reload();
          selected.reload();
          void onChanged();
        }}
      >
        Odśwież powiązania incydentów
      </button>
      <PageFeedback
        loading={p.loading}
        error={p.error}
        empty={!p.items.length}
      />
      {error && (
        <p role="alert">
          {error} Odczytaj aktualne wersje przed kolejną próbą.
        </p>
      )}
      {notice && <p role="status">{notice}</p>}
      {uncertain && (
        <p role="status">
          Nie potwierdzono zapisu.{" "}
          <button disabled={busy} onClick={() => void change(undefined, false)}>
            Potwierdź powiązanie
          </button>
        </p>
      )}
      <ul>
        {p.items.map((i) => (
          <li key={i.id}>
            <Link to={`/incidents/${i.id}`}>
              {i.number} · {i.title}
            </Link>{" "}
            · {i.state}
            {record.can_edit && (
              <button
                disabled={busy || uncertain}
                onClick={async () => {
                  try {
                    const current = await api<SourceIncident>(
                      `/api/v1/problems/source-incidents/${i.id}`,
                    );
                    await change(current, true);
                  } catch (e) {
                    setError(errorText(e));
                  }
                }}
              >
                Odłącz incydent
              </button>
            )}
          </li>
        ))}
      </ul>
      <More loading={p.loading} next={p.next} load={p.more} />
      {record.can_edit && (
        <form
          onSubmit={(e) => {
            e.preventDefault();
            if (selected.data) void change(selected.data, false);
          }}
        >
          <InventoryChoice
            label="Powiązany incydent"
            endpoint="/api/v1/problems/lookups/incidents"
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
              <Link to={`/incidents/${selected.data.id}`}>
                {selected.data.number} · {selected.data.title}
              </Link>{" "}
              · {selected.data.state} · Wersja {selected.data.version}
            </p>
          )}
          <button disabled={busy || uncertain || !selected.data?.can_link}>
            Powiąż incydent
          </button>
        </form>
      )}
    </section>
  );
}
export function ProblemTasks({
  record,
  blocked,
  onChanged,
  retain,
}: {
  record: Problem;
  blocked: boolean;
  onChanged: () => Promise<void>;
  retain: (tab: string, needed: boolean) => void;
}) {
  const p = useRemotePage<AnalyticalTask>(
      `/api/v1/problems/${record.id}/tasks?limit=25`,
      "items",
      "before_id",
    ),
    [manual, setSelected] = useState<AnalyticalTask | "new">();
  const [params, setParams] = useSearchParams(),
    requested = params.get("task_id");
  const target = useIncidentQuery<AnalyticalTask>(
    requested && /^[1-9]\d*$/.test(requested)
      ? `/api/v1/problems/${record.id}/tasks/${requested}`
      : null,
  );
  const selected =
    manual ?? (requested ? (target.data ?? undefined) : undefined);
  function closeTarget() {
    setSelected(undefined);
    if (requested) {
      const n = new URLSearchParams(params);
      n.delete("task_id");
      setParams(n, { replace: true });
    }
    retain("tasks", false);
  }

  return (
    <section>
      <h3>Zadania analityczne</h3>
      {requested &&
        (target.loading ? (
          <p role="status">Odczyt wskazanego zadania…</p>
        ) : target.error || !/^[1-9]\d*$/.test(requested) ? (
          <div role="alert">
            <p>{target.error || "Nieprawidłowy identyfikator zadania."}</p>
            <button onClick={target.reload}>Ponów odczyt zadania</button>
            <button onClick={closeTarget}>Wróć do listy zadań</button>
          </div>
        ) : null)}
      <div className="action-row">
        <button disabled={!!selected} onClick={p.reload}>
          Odśwież zadania
        </button>
        {record.can_tasks && (
          <button
            disabled={blocked || !!selected}
            onClick={() => setSelected("new")}
          >
            Nowe zadanie analityczne
          </button>
        )}
      </div>
      <PageFeedback
        loading={p.loading}
        error={p.error}
        empty={!p.items.length}
      />
      <div className="table-scroll">
        <table className="record-table">
          <thead>
            <tr>
              <th>Numer</th>
              <th>Opis</th>
              <th>Stan</th>
              <th>Przypisanie</th>
              <th>Wymagane</th>
              <th>Akcje</th>
            </tr>
          </thead>
          <tbody>
            {p.items.map((t) => (
              <tr key={t.id}>
                <td>{t.number}</td>
                <td>{t.title}</td>
                <td>{labels[t.state]}</td>
                <td>
                  {t.group_name} · {t.assigned_name || "Nieprzypisany"}
                </td>
                <td>{t.required ? "Tak" : "Nie"}</td>
                <td>
                  <button disabled={!!selected} onClick={() => setSelected(t)}>
                    {t.can_edit && t.state !== "COMPLETED"
                      ? "Otwórz zadanie"
                      : "Podgląd zadania"}
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <More loading={p.loading} next={p.next} load={p.more} />
      {requested && !manual && target.data?.can_edit && (
        <button
          disabled={blocked}
          onClick={() => {
            const task = target.data!;
            const n = new URLSearchParams(params);
            n.delete("task_id");
            setParams(n, { replace: true });
            setSelected(task);
          }}
        >
          Edytuj wskazane zadanie
        </button>
      )}
      {selected && (
        <TaskEditor
          key={
            selected === "new"
              ? "new"
              : `${manual ? "edit" : "preview"}:${selected.id}`
          }
          record={record}
          initial={
            selected === "new"
              ? undefined
              : manual
                ? selected
                : { ...selected, can_edit: false }
          }
          disabled={blocked}
          retain={retain}
          refresh={onChanged}
          close={() => {
            closeTarget();
          }}
          saved={async () => {
            await onChanged();
            p.reload();
            closeTarget();
          }}
        />
      )}
    </section>
  );
}
function TaskEditor({
  record,
  initial,
  disabled,
  retain,
  close,
  saved,
  refresh,
}: {
  record: Problem;
  initial?: AnalyticalTask;
  disabled: boolean;
  retain: (tab: string, needed: boolean) => void;
  close: () => void;
  saved: () => Promise<void>;
  refresh: () => Promise<void>;
}) {
  const empty: AnalyticalTask = {
    id: "new",
    problem_id: record.id,
    number: "Nowe zadanie",
    title: "",
    instructions: "",
    required: true,
    state: "OPEN",
    result: "",
    version: "0",
    assigned_to: null,
    assignment_group_id: record.assignment_group_id,
    assigned_name: "",
    group_name: record.group_name,
    can_edit: record.can_tasks,
    updated_at: "",
  };
  const base = initial ?? empty,
    path = `/api/v1/problems/${record.id}/tasks`,
    editor = useEditor(base, `${path}/${base.id}`),
    creation = useCreation<AnalyticalTask>();
  const taskFields = (t: AnalyticalTask) => ({
    title: t.title,
    instructions: t.instructions,
    required: t.required,
    state: t.state,
    result: t.result,
    assigned_to: t.assigned_to,
    assignment_group_id: t.assignment_group_id,
  });
  const [draft, setDraft] = useState(taskFields(base)),
    [version, setVersion] = useState(base.version),
    [parentVersion, setParentVersion] = useState(record.version);
  const [names, setNames] = useState<Record<string, string>>({
      ["u:" + base.assigned_to]: base.assigned_name,
      ["g:" + base.assignment_group_id]: base.group_name,
    }),
    operation = useRef<{ body: string; key: string } | undefined>(undefined);
  const [readError, setReadError] = useState("");
  const dirty = JSON.stringify(draft) !== JSON.stringify(taskFields(base));
  useEffect(
    () => retain("tasks", dirty || creation.uncertain || editor.uncertain),
    [dirty, creation.uncertain, editor.uncertain, retain],
  );
  useEffect(() => () => retain("tasks", false), [retain]);
  const busy = editor.busy || creation.busy,
    uncertain = editor.uncertain || creation.uncertain,
    readonly = disabled || !record.can_tasks || base.state === "COMPLETED";
  function set(key: keyof typeof draft, value: unknown) {
    setDraft((d) => ({ ...d, [key]: value }));
  }
  async function submit() {
    const body = {
      ...draft,
      version: initial ? version : undefined,
      problem_version: parentVersion,
    };
    if (initial) {
      const serialized = JSON.stringify(body);
      if (
        !operation.current ||
        (operation.current.body !== serialized && !editor.uncertain)
      )
        operation.current = { body: serialized, key: crypto.randomUUID() };
      if (
        await editor.save(`${path}/${base.id}`, body, "PUT", {
          "Idempotency-Key": operation.current.key,
        })
      )
        await saved();
    } else if (await creation.create(path, body)) await saved();
  }
  return (
    <section className="panel">
      <h4>{base.number}</h4>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          void submit();
        }}
      >
        <fieldset
          className="form-fields"
          disabled={readonly || busy || uncertain || !!editor.conflict}
        >
          <div className="form-grid">
            <label>
              Opis zadania
              <input
                required
                maxLength={160}
                value={draft.title}
                onChange={(e) => set("title", e.target.value)}
              />
            </label>
            <label>
              Stan zadania
              <select
                aria-label="Stan zadania analitycznego"
                value={draft.state}
                onChange={(e) => set("state", e.target.value)}
              >
                {[
                  base.state,
                  ...(base.state === "OPEN"
                    ? ["IN_PROGRESS", "CANCELLED"]
                    : base.state === "IN_PROGRESS"
                      ? ["COMPLETED", "CANCELLED"]
                      : base.state === "CANCELLED"
                        ? ["OPEN"]
                        : []),
                ].map((v) => (
                  <option key={v} value={v}>
                    {labels[v]}
                  </option>
                ))}
              </select>
            </label>
            <InventoryChoice
              label="Grupa zadania"
              endpoint="/api/v1/problems/lookups/groups"
              value={
                draft.assignment_group_id
                  ? {
                      id: draft.assignment_group_id,
                      label:
                        names["g:" + draft.assignment_group_id] ||
                        `Grupa #${draft.assignment_group_id}`,
                    }
                  : null
              }
              disabled={readonly || busy || uncertain}
              onChange={(v) => {
                if (v) setNames((n) => ({ ...n, ["g:" + v.id]: v.label }));
                set("assignment_group_id", v?.id ?? null);
              }}
            />
            <InventoryChoice
              label="Analityk zadania"
              endpoint="/api/v1/problems/lookups/users"
              value={
                draft.assigned_to
                  ? {
                      id: draft.assigned_to,
                      label:
                        names["u:" + draft.assigned_to] ||
                        `Użytkownik #${draft.assigned_to}`,
                    }
                  : null
              }
              disabled={readonly || busy || uncertain}
              onChange={(v) => {
                if (v) setNames((n) => ({ ...n, ["u:" + v.id]: v.label }));
                set("assigned_to", v?.id ?? null);
              }}
            />
          </div>
          <label>
            Instrukcje analizy
            <textarea
              required
              rows={3}
              maxLength={20000}
              value={draft.instructions}
              onChange={(e) => set("instructions", e.target.value)}
            />
          </label>
          <label>
            Wynik zadania
            <textarea
              rows={3}
              maxLength={20000}
              value={draft.result}
              onChange={(e) => set("result", e.target.value)}
            />
          </label>
          <label className="checkbox-label">
            <input
              type="checkbox"
              checked={draft.required}
              onChange={(e) => set("required", e.target.checked)}
            />
            Wymagane przed rozwiązaniem problemu
          </label>
        </fieldset>
        {(editor.error || creation.error || readError) && (
          <>
            <p role="alert">{readError || editor.error || creation.error}</p>
            {!uncertain && !editor.conflict && (
              <button
                type="button"
                disabled={busy}
                onClick={async () => {
                  setReadError("");
                  try {
                    const current = await api<Problem>(
                      `/api/v1/problems/${record.id}`,
                    );
                    await refresh();
                    setParentVersion(current.version);
                  } catch (e) {
                    setReadError(errorText(e));
                  }
                }}
              >
                Odczytaj aktualny problem i zachowaj szkic zadania
              </button>
            )}
          </>
        )}
        {editor.conflict && (
          <section aria-label="Konflikt zadania">
            <h4>Konflikt wersji zadania</h4>
            <div className="conflict-columns">
              <pre>{JSON.stringify(draft, null, 2)}</pre>
              <pre>{JSON.stringify(taskFields(editor.conflict), null, 2)}</pre>
            </div>
            <button
              type="button"
              onClick={() => {
                setVersion(editor.conflict!.version);
                setParentVersion(record.version);
                editor.resolve();
              }}
            >
              Zachowaj propozycję zadania
            </button>
            <button
              type="button"
              onClick={() => {
                setDraft(taskFields(editor.conflict!));
                setVersion(editor.conflict!.version);
                setParentVersion(record.version);
                editor.resolve();
              }}
            >
              Użyj aktualnego zadania
            </button>
          </section>
        )}
        {uncertain && (
          <p role="status">Ponowienie zachowa treść i klucz ostatniej próby.</p>
        )}
        <div className="action-row">
          {!readonly && (
            <button
              className="button button--primary"
              disabled={
                busy ||
                !!editor.conflict ||
                (!uncertain &&
                  (!draft.title.trim() ||
                    !draft.instructions.trim() ||
                    !draft.assignment_group_id))
              }
            >
              {uncertain
                ? "Potwierdź wynik zadania"
                : initial
                  ? "Zapisz zadanie"
                  : "Utwórz zadanie analityczne"}
            </button>
          )}
          <button
            type="button"
            disabled={busy || uncertain}
            onClick={() => {
              if (
                !dirty ||
                window.confirm("Odrzucić niezapisany szkic zadania?")
              )
                close();
            }}
          >
            Zamknij formularz zadania
          </button>
        </div>
      </form>
    </section>
  );
}
