import { ConflictComparison } from "../components/ConflictComparison";
import { useRetainedCommand } from "../components/useRetainedCommand";
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
  type ExecutionTask,
  type Change,
  type SourceProblem,
} from "./contracts";

export function ChangeProblems({
  record,
  onChanged,
  retain,
}: {
  record: Change;
  onChanged: () => Promise<void>;
  retain?: (tab: string, needed: boolean) => void;
}) {
  const path = `/api/v1/changes/${record.id}/problems`,
    p = useRemotePage<SourceProblem>(path + "?limit=25", "items", "before_id");
  const [choice, setChoice] = useState<Choice | null>(null),
    [busy, setBusy] = useState(false),
    [error, setError] = useState(""),
    [notice, setNotice] = useState(""),
    [uncertain, setUncertain] = useState(false);
  const selected = useIncidentQuery<SourceProblem>(
    choice ? `/api/v1/changes/source-problems/${choice.id}` : null,
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
  useRetainedCommand(retain, "related_problems", busy || uncertain);
  const inFlight = useRef(false);
  async function change(problem: SourceProblem | undefined, remove: boolean) {
    if (inFlight.current || (!problem && !operation.current)) return;
    inFlight.current = true;
    setBusy(true);
    setError("");
    const c = new AbortController();
    controller.current = c;
    try {
      let proposal =
        uncertain && operation.current
          ? operation.current
          : {
              path: `${path}/${problem!.id}`,
              body: jsonBody({
                version: record.version,
                problem_version: problem!.version,
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
      setNotice("Zapisano powiązanie. Stan problemu pozostaje bez zmian.");
      p.reload();
      await onChanged();
    } catch (e) {
      if (alive.current && !c.signal.aborted) {
        setError(errorText(e));
        setUncertain(!(e instanceof ApiError) || e.status >= 500);
      }
    } finally {
      inFlight.current = false;
      if (alive.current) setBusy(false);
    }
  }
  return (
    <section>
      <h3>Powiązane problemy</h3>
      <button
        disabled={busy || uncertain}
        onClick={() => {
          p.reload();
          selected.reload();
          void onChanged();
        }}
      >
        Odśwież powiązania problemów
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
            <Link to={`/problems/${i.id}`}>
              {i.number} · {i.title}
            </Link>{" "}
            · {i.state}
            {record.can_edit && (
              <button
                disabled={busy || uncertain}
                onClick={async () => {
                  try {
                    const current = await api<SourceProblem>(
                      `/api/v1/changes/source-problems/${i.id}`,
                    );
                    await change(current, true);
                  } catch (e) {
                    setError(errorText(e));
                  }
                }}
              >
                Odłącz problem
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
            label="Powiązany problem"
            endpoint="/api/v1/changes/lookups/problems"
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
              <Link to={`/problems/${selected.data.id}`}>
                {selected.data.number} · {selected.data.title}
              </Link>{" "}
              · {selected.data.state} · Wersja {selected.data.version}
            </p>
          )}
          <button disabled={busy || uncertain || !selected.data?.can_link}>
            Powiąż problem
          </button>
        </form>
      )}
    </section>
  );
}
export function ChangeTasks({
  record,
  blocked,
  onChanged,
  retain,
}: {
  record: Change;
  blocked: boolean;
  onChanged: () => Promise<void>;
  retain: (tab: string, needed: boolean) => void;
}) {
  const p = useRemotePage<ExecutionTask>(
      `/api/v1/changes/${record.id}/tasks?limit=25`,
      "items",
      "before_id",
    ),
    [manual, setSelected] = useState<ExecutionTask | "new">();
  const [params, setParams] = useSearchParams(),
    requested = params.get("task_id");
  const target = useIncidentQuery<ExecutionTask>(
    requested && /^[1-9]\d*$/.test(requested)
      ? `/api/v1/changes/${record.id}/tasks/${requested}`
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
      <h3>Zadania wykonawcze</h3>
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
            Nowe zadanie wykonawcze
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
  record: Change;
  initial?: ExecutionTask;
  disabled: boolean;
  retain: (tab: string, needed: boolean) => void;
  close: () => void;
  saved: () => Promise<void>;
  refresh: () => Promise<void>;
}) {
  const empty: ExecutionTask = {
    id: "new",
    change_id: record.id,
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
    path = `/api/v1/changes/${record.id}/tasks`,
    editor = useEditor(base, `${path}/${base.id}`),
    creation = useCreation<ExecutionTask>();
  const current = editor.record;
  const taskFields = (t: ExecutionTask) => ({
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
  const dirty = JSON.stringify(draft) !== JSON.stringify(taskFields(current));
  useEffect(
    () =>
      retain(
        "tasks",
        dirty ||
          creation.busy ||
          creation.uncertain ||
          editor.busy ||
          !!editor.conflict ||
          editor.uncertain,
      ),
    [
      dirty,
      creation.busy,
      creation.uncertain,
      editor.busy,
      editor.conflict,
      editor.uncertain,
      retain,
    ],
  );
  useEffect(() => () => retain("tasks", false), [retain]);
  const busy = editor.busy || creation.busy,
    uncertain = editor.uncertain || creation.uncertain,
    readonly =
      disabled ||
      !record.can_tasks ||
      !current.can_edit ||
      current.state === "COMPLETED";
  const allowedStates = [
    current.state,
    ...(current.state === "OPEN"
      ? [...(record.state === "IMPLEMENT" ? ["IN_PROGRESS"] : []), "CANCELLED"]
      : current.state === "IN_PROGRESS"
        ? ["COMPLETED", "CANCELLED"]
        : current.state === "CANCELLED"
          ? ["OPEN"]
          : []),
  ];
  function set(key: keyof typeof draft, value: unknown) {
    setDraft((d) => ({ ...d, [key]: value }));
  }
  const [conflictLoading, setConflictLoading] = useState(false);
  async function resolveTaskConflict(keepProposal: boolean) {
    if (!editor.conflict || conflictLoading) return;
    setConflictLoading(true);
    setReadError("");
    try {
      const parent = await api<Change>(`/api/v1/changes/${record.id}`);
      await refresh();
      if (!keepProposal) setDraft(taskFields(editor.conflict));
      setVersion(editor.conflict.version);
      setParentVersion(parent.version);
      editor.resolve();
    } catch (e) {
      setReadError(errorText(e));
    } finally {
      setConflictLoading(false);
    }
  }
  async function submit() {
    const body = {
      ...draft,
      version: initial ? version : undefined,
      change_version: parentVersion,
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
                aria-label="Stan zadania wykonawczego"
                value={draft.state}
                onChange={(e) => set("state", e.target.value)}
              >
                {[...new Set([...allowedStates, draft.state])].map((v) => (
                  <option
                    key={v}
                    value={v}
                    disabled={!allowedStates.includes(v)}
                  >
                    {labels[v]}
                    {!allowedStates.includes(v) ? " (propozycja)" : ""}
                  </option>
                ))}
              </select>
            </label>
            <InventoryChoice
              label="Grupa zadania"
              endpoint="/api/v1/changes/lookups/groups"
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
              endpoint="/api/v1/changes/lookups/users"
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
            Instrukcje wykonania
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
            Wymagane przed przeglądem i zamknięciem zmiany
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
                    const current = await api<Change>(
                      `/api/v1/changes/${record.id}`,
                    );
                    await refresh();
                    setParentVersion(current.version);
                  } catch (e) {
                    setReadError(errorText(e));
                  }
                }}
              >
                Odczytaj aktualną zmianę i zachowaj szkic zadania
              </button>
            )}
          </>
        )}
        {editor.conflict && (
          <section aria-label="Konflikt zadania">
            <h4>Konflikt wersji zadania</h4>
            <ConflictComparison
              proposal={draft}
              current={taskFields(editor.conflict)}
            />
            <button
              type="button"
              disabled={conflictLoading}
              onClick={() => void resolveTaskConflict(true)}
            >
              Zachowaj propozycję zadania
            </button>
            <button
              type="button"
              disabled={conflictLoading}
              onClick={() => void resolveTaskConflict(false)}
            >
              Użyj aktualnego zadania
            </button>
          </section>
        )}
        {uncertain && (
          <p role="status">Ponowienie zachowa treść i klucz ostatniej próby.</p>
        )}
        {readonly && dirty && !editor.conflict && (
          <section aria-label="Niezapisana propozycja zadania">
            <p role="status">
              Aktualny stan lub uprawnienia zadania nie pozwalają zapisać
              propozycji. Możesz ją odczytać lub skopiować przed zamknięciem.
            </p>
            <ConflictComparison
              proposal={draft}
              current={taskFields(current)}
            />
          </section>
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
                  : "Utwórz zadanie wykonawcze"}
            </button>
          )}
          <button
            type="button"
            disabled={busy || uncertain || conflictLoading}
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
