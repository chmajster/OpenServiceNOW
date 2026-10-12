import { useState } from "react";
import { flushSync } from "react-dom";
import { Link, useSearchParams } from "react-router-dom";
import { useAuth } from "../auth/AuthContext";
import { useRemotePage } from "../components/useRemotePage";
import { RemoteCombobox, type Choice } from "../components/RemoteCombobox";
import { useIncidentQuery } from "../incidents/hooks";
import { UnsavedGuard } from "../incidents/UnsavedGuard";
import { LoadState } from "../requests/LoadState";
import { useEditor } from "../requests/useEditor";
import { useCreation } from "../requests/useCreation";
import {
  blank,
  labels,
  errors,
  type Config,
  type Definition,
  type ProcessNode,
  type Execution,
} from "../workflow/contracts";
const base = "/api/v1/workflow";
export function WorkflowPage() {
  const { hasPermission: has } = useAuth();
  const [p, set] = useSearchParams(),
    tab =
      p.get("tab") === "executions" ||
      (!has("workflow.author") && !has("workflow.publish"))
        ? "executions"
        : "definitions",
    id = p.get("id");
  const [creating, setCreating] = useState(false);
  const defs = useRemotePage<Definition>(
    `${base}/definitions?limit=25&q=${encodeURIComponent(p.get("q") ?? "")}`,
    "items",
    "after_id",
    tab === "definitions" &&
      (has("workflow.author") || has("workflow.publish")),
  );
  const detail = useIncidentQuery<Definition>(
    id ? `${base}/definitions/${id}` : null,
  );
  const runs = useRemotePage<Execution>(
    `${base}/executions?limit=25&state=${encodeURIComponent(p.get("state") ?? "")}`,
    "items",
    "before_id",
    tab === "executions" && has("workflow.manage"),
  );
  function select(id?: string) {
    setCreating(false);
    const next = new URLSearchParams(p);
    if (id) next.set("id", id);
    else next.delete("id");
    set(next);
  }
  if (
    !has("workflow.author") &&
    !has("workflow.publish") &&
    !has("workflow.manage")
  )
    return <LoadState error="Brak uprawnień do administracji procesami." />;
  return (
    <>
      <h1>Procesy i akceptacje</h1>
      <nav className="tabs" aria-label="Administracja procesami">
        {has("workflow.author") || has("workflow.publish") ? (
          <button
            className="button button--secondary"
            aria-current={tab === "definitions" ? "page" : undefined}
            onClick={() => {
              setCreating(false);
              set({ tab: "definitions" });
            }}
          >
            Definicje
          </button>
        ) : null}
        {has("workflow.manage") ? (
          <button
            className="button button--secondary"
            aria-current={tab === "executions" ? "page" : undefined}
            onClick={() => {
              setCreating(false);
              set({ tab: "executions" });
            }}
          >
            Wykonania
          </button>
        ) : null}
      </nav>
      {tab === "executions" ? (
        <>
          <label>
            Stan procesu
            <select
              value={p.get("state") ?? ""}
              onChange={(e) =>
                set({ tab: "executions", state: e.target.value })
              }
            >
              <option value="">Wszystkie</option>
              {["READY", "WAITING", "ERROR", "COMPLETED", "CANCELLED"].map(
                (s) => (
                  <option key={s} value={s}>
                    {labels[s]}
                  </option>
                ),
              )}
            </select>
          </label>
          <LoadState
            loading={runs.loading}
            error={runs.error}
            retry={runs.retry}
          />
          <div className="table-scroll">
            <table>
              <thead>
                <tr>
                  <th>Wniosek</th>
                  <th>Proces</th>
                  <th>Krok</th>
                  <th>Stan</th>
                  <th>Przyczyna</th>
                </tr>
              </thead>
              <tbody>
                {runs.items.map((x) => (
                  <tr key={x.id}>
                    <td>
                      <Link to={`/approvals/${x.id}`}>{x.request_number}</Link>
                    </td>
                    <td>
                      {x.name} · {x.definition_version}
                    </td>
                    <td>{x.current_node}</td>
                    <td>{labels[x.state]}</td>
                    <td>
                      {x.error_code
                        ? (errors[x.error_code] ?? x.error_code)
                        : "—"}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          {!runs.loading && !runs.error && !runs.items.length ? (
            <p role="status">Brak wykonań procesów.</p>
          ) : null}
          {runs.next ? (
            <button
              className="button button--secondary"
              disabled={runs.loading}
              onClick={() => void runs.more()}
            >
              Więcej wykonań
            </button>
          ) : null}
          <button
            className="button button--secondary"
            onClick={() => void runs.retry()}
          >
            Odśwież wykonania
          </button>
        </>
      ) : creating ? (
        <Editor
          key="new"
          initial={{ id: "", version: "0", draft: structuredClone(blank) }}
          back={() => setCreating(false)}
          saved={(d) => {
            select(d.id);
            defs.reload();
          }}
        />
      ) : id ? (
        <>
          <LoadState
            loading={detail.loading}
            error={detail.error}
            retry={detail.reload}
          />
          {detail.data ? (
            <Editor
              key={id}
              initial={detail.data}
              back={() => select()}
              saved={() => defs.reload()}
            />
          ) : null}
        </>
      ) : (
        <>
          {has("workflow.author") ? (
            <button
              className="button button--primary"
              onClick={() => setCreating(true)}
            >
              Nowy proces
            </button>
          ) : null}
          <label>
            Wyszukaj proces
            <input
              maxLength={200}
              value={p.get("q") ?? ""}
              onChange={(e) =>
                set(
                  { tab: "definitions", q: e.target.value },
                  { replace: true },
                )
              }
            />
          </label>
          <LoadState
            loading={defs.loading}
            error={defs.error}
            retry={defs.retry}
          />
          <div className="table-scroll">
            <table>
              <thead>
                <tr>
                  <th>Proces</th>
                  <th>Szkic</th>
                  <th>Publikacja</th>
                </tr>
              </thead>
              <tbody>
                {defs.items.map((d) => (
                  <tr key={d.id}>
                    <td>
                      <button
                        className="link-button"
                        onClick={() => select(d.id)}
                      >
                        {d.draft.name}
                      </button>
                    </td>
                    <td>{d.version}</td>
                    <td>{d.published_version ?? "Brak"}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          {!defs.loading && !defs.error && !defs.items.length ? (
            <p role="status">Brak definicji procesów.</p>
          ) : null}
          {defs.next ? (
            <button
              className="button button--secondary"
              onClick={() => void defs.more()}
            >
              Więcej definicji
            </button>
          ) : null}
        </>
      )}
    </>
  );
}
function Editor({
  initial,
  back,
  saved,
}: {
  initial: Definition;
  back: () => void;
  saved: (d: Definition) => void;
}) {
  const { hasPermission: has } = useAuth();
  const ed = useEditor(initial, `${base}/definitions/${initial.id}`),
    creation = useCreation<Definition>();
  const [draft, setDraft] = useState<Config>(() =>
      structuredClone(initial.draft),
    ),
    [dirty, setDirty] = useState(false);
  function edit(p: Partial<Config>) {
    setDraft({ ...draft, ...p });
    setDirty(true);
  }
  async function save() {
    if (!initial.id) {
      const d = await creation.create(`${base}/definitions`, {
        version: "0",
        draft,
      });
      if (d) {
        flushSync(() => setDirty(false));
        saved(d);
      }
    } else {
      if (
        await ed.save(
          `${base}/definitions/${initial.id}`,
          { version: ed.record.version, draft },
          "PUT",
        )
      ) {
        setDirty(false);
        saved(ed.record);
      }
    }
  }
  const locked =
    ed.busy || creation.busy || !!ed.conflict || creation.uncertain;
  return (
    <>
      <UnsavedGuard dirty={dirty} includeSearch />
      <button className="button button--secondary" onClick={back}>
        Lista procesów
      </button>
      <form
        className="panel"
        onSubmit={(e) => {
          e.preventDefault();
          void save();
        }}
      >
        <h2>{initial.id ? "Szkic procesu" : "Nowy proces"}</h2>
        <p>
          Szkic {ed.record.version}; opublikowana wersja{" "}
          {ed.record.published_version ?? "brak"}. Uruchomione wykonania
          zachowują swoją wersję.
        </p>
        <p>
          Wykonawca musi mieć workflow.execute, request.fulfill i aktywne
          członkostwo w grupie realizacji. Edycja procesu nie nadaje tych
          uprawnień.
        </p>
        <fieldset disabled={locked || !has("workflow.author")}>
          <label>
            Nazwa procesu
            <input
              required
              maxLength={200}
              value={draft.name}
              onChange={(e) => edit({ name: e.target.value })}
            />
          </label>
          <Identity
            kind="users"
            label="Tożsamość wykonawcza"
            id={draft.executor_id}
            change={(id) => edit({ executor_id: id })}
          />
          <Identity
            kind="groups"
            label="Grupa realizacji procesu"
            id={draft.group_id}
            change={(id) => edit({ group_id: id })}
          />
          <label>
            Akceptacja własnego wniosku
            <select
              value={draft.self_approval}
              onChange={(e) => edit({ self_approval: e.target.value })}
            >
              <option value="DENY">Zabroniona</option>
              <option value="ALLOW">
                Dozwolona dla uprawnionego uczestnika
              </option>
            </select>
          </label>
          <p>
            Grupa akceptujących: lista uczestników przy wejściu w krok, do 100
            osób; decyzja wymaga bieżącego członkostwa i workflow.approve. Nowi
            członkowie nie dołączają. W polityce „wszyscy” usunięty uczestnik
            nie zmniejsza wymaganej liczby decyzji; obowiązuje zapisany termin.
          </p>
          {draft.nodes.map((n, i) => (
            <NodeForm
              key={i}
              node={n}
              nodes={draft.nodes}
              change={(next) =>
                edit({ nodes: draft.nodes.map((v, j) => (i === j ? next : v)) })
              }
              remove={() =>
                edit({ nodes: draft.nodes.filter((_, j) => j !== i) })
              }
            />
          ))}
          <button
            type="button"
            className="button button--secondary"
            disabled={draft.nodes.length >= 50}
            onClick={() => {
              const id = Array.from(
                { length: 51 },
                (_, i) => `step_${i + 1}`,
              ).find((id) => !draft.nodes.some((n) => n.id === id))!;
              edit({
                nodes: [
                  ...draft.nodes,
                  {
                    id,
                    label: "Nowy krok",
                    type: "WAIT_TIMER",
                    seconds: 60,
                  },
                ],
              });
            }}
          >
            Dodaj krok
          </button>
        </fieldset>
        <LoadState error={ed.error ?? creation.error} />
        {ed.notice ? <p role="status">{ed.notice}</p> : null}
        {ed.conflict ? (
          <section className="conflict-panel">
            <h3>Konflikt wersji</h3>
            <p>
              Twoja definicja pozostaje w formularzu. Aktualny szkic:{" "}
              {ed.conflict.version}.
            </p>
            <details>
              <summary>Aktualny szkic</summary>
              <pre>{JSON.stringify(ed.conflict.draft, null, 2)}</pre>
            </details>
            <details>
              <summary>Lokalna propozycja</summary>
              <pre>{JSON.stringify(draft, null, 2)}</pre>
            </details>
            <button
              type="button"
              className="button button--secondary"
              onClick={() => {
                if (has("workflow.author")) setDirty(true);
                else setDraft(ed.conflict!.draft);
                ed.resolve();
              }}
            >
              {has("workflow.author")
                ? "Porównano — zachowaj propozycję z aktualną wersją"
                : "Porównano — wczytaj aktualny szkic"}
            </button>
          </section>
        ) : null}
        <div className="action-row">
          {has("workflow.author") ? (
            <button
              className="button button--primary"
              disabled={ed.busy || creation.busy || !!ed.conflict}
              type="submit"
            >
              {creation.uncertain
                ? "Potwierdź wynik utworzenia"
                : "Zapisz szkic procesu"}
            </button>
          ) : null}
          {initial.id && has("workflow.publish") ? (
            <button
              type="button"
              className="button button--secondary"
              disabled={locked || dirty}
              onClick={async () => {
                const ok = await ed.save(
                  `${base}/definitions/${initial.id}/publish`,
                  {
                    version: ed.record.version,
                  },
                );
                if (ok) saved(ed.record);
              }}
            >
              Zweryfikuj i opublikuj
            </button>
          ) : null}
        </div>
      </form>
    </>
  );
}
function Identity({
  kind,
  label,
  id,
  change,
}: {
  kind: "users" | "groups";
  label: string;
  id: string;
  change: (id: string) => void;
}) {
  const [selected, set] = useState<Choice | null>(
    id && id !== "0" ? { id, label: `#${id}` } : null,
  );
  return (
    <RemoteCombobox
      kind={kind}
      label={label}
      endpoint={`${base}/lookups/${kind}`}
      value={
        selected?.id === id
          ? selected
          : id && id !== "0"
            ? { id, label: `#${id}` }
            : null
      }
      onChange={(v) => {
        set(v);
        change(v?.id ?? "0");
      }}
    />
  );
}
function NodeForm({
  node: n,
  nodes,
  change,
  remove,
}: {
  node: ProcessNode;
  nodes: ProcessNode[];
  change: (n: ProcessNode) => void;
  remove: () => void;
}) {
  const edit = (p: Partial<ProcessNode>) => change({ ...n, ...p });
  const route = (
    label: string,
    key: "next" | "otherwise" | "rejected" | "timeout" | "task_ref",
  ) => (
    <label>
      {label}
      <select
        required
        value={n[key] ?? ""}
        onChange={(e) => edit({ [key]: e.target.value })}
      >
        <option value="">Wybierz krok…</option>
        {nodes
          .filter(
            (v) =>
              v.id !== n.id && (key !== "task_ref" || v.type === "CREATE_TASK"),
          )
          .map((v, i) => (
            <option key={i} value={v.id}>
              {v.label} ({v.id})
            </option>
          ))}
      </select>
    </label>
  );
  return (
    <fieldset className="workflow-node-editor">
      <legend>
        {n.label} ({n.id})
      </legend>
      <div className="form-grid">
        <label>
          Identyfikator kroku
          <input
            required
            pattern="[a-z][a-z0-9_]{0,47}"
            value={n.id}
            onChange={(e) => edit({ id: e.target.value })}
          />
        </label>
        <label>
          Etykieta kroku
          <input
            required
            maxLength={160}
            value={n.label}
            onChange={(e) => edit({ label: e.target.value })}
          />
        </label>
        <label>
          Typ kroku
          <select
            value={n.type}
            onChange={(e) => {
              const type = e.target.value;
              change({
                id: n.id,
                label: n.label,
                type,
                ...(type !== "END" ? { next: n.next } : {}),
                ...(type === "NOTIFY" ? { audience: "REQUESTER" } : {}),
                ...(["APPROVAL", "WAIT_TASK", "WAIT_TIMER"].includes(type)
                  ? { seconds: 86400 }
                  : {}),
                ...(type === "APPROVAL"
                  ? { target_type: "GROUP", target_id: "0", policy: "ANY" }
                  : {}),
                ...(type === "UPDATE_RECORD"
                  ? { state: "IN_PROGRESS", reason: "" }
                  : {}),
              });
            }}
          >
            {[
              "START",
              "CONDITION",
              "APPROVAL",
              "CREATE_TASK",
              "WAIT_TASK",
              "WAIT_TIMER",
              "UPDATE_RECORD",
              "NOTIFY",
              "END",
            ].map((t) => (
              <option key={t} value={t}>
                {labels[t]}
              </option>
            ))}
          </select>
        </label>
        {n.type !== "END"
          ? route(
              n.type === "APPROVAL" ? "Po akceptacji" : "Następny krok",
              "next",
            )
          : null}
        {n.type === "CONDITION" ? (
          <>
            <label>
              Pole formularza
              <input
                required
                value={n.field ?? ""}
                onChange={(e) => edit({ field: e.target.value })}
              />
            </label>
            <label>
              Wartość porównania
              {typeof n.equals === "boolean" ? (
                <select
                  value={String(n.equals)}
                  onChange={(e) => edit({ equals: e.target.value === "true" })}
                >
                  <option value="true">Tak</option>
                  <option value="false">Nie</option>
                </select>
              ) : (
                <input
                  required
                  type={typeof n.equals === "number" ? "number" : "text"}
                  value={
                    typeof n.equals === "string"
                      ? n.equals
                      : String(n.equals ?? "")
                  }
                  onChange={(e) => {
                    const v = e.target.value;
                    edit({
                      equals:
                        typeof n.equals === "boolean"
                          ? v === "true"
                          : typeof n.equals === "number"
                            ? Number(v)
                            : v,
                    });
                  }}
                />
              )}
            </label>
            <label>
              Typ wartości
              <select
                value={n.equals === undefined ? "string" : typeof n.equals}
                onChange={(e) =>
                  edit({
                    equals:
                      e.target.value === "boolean"
                        ? false
                        : e.target.value === "number"
                          ? 0
                          : "",
                  })
                }
              >
                <option value="string">Tekst lub data</option>
                <option value="number">Liczba</option>
                <option value="boolean">Checkbox (true/false)</option>
              </select>
            </label>
            {route("Warunek niespełniony", "otherwise")}
          </>
        ) : null}
        {n.type === "APPROVAL" ? (
          <>
            <label>
              Cel akceptacji
              <select
                value={n.target_type ?? "GROUP"}
                onChange={(e) =>
                  edit({ target_type: e.target.value, target_id: "0" })
                }
              >
                <option value="USER">Użytkownik</option>
                <option value="GROUP">Grupa</option>
              </select>
            </label>
            <Identity
              key={n.target_type}
              kind={n.target_type === "USER" ? "users" : "groups"}
              label="Akceptujący"
              id={n.target_id ?? "0"}
              change={(id) => edit({ target_id: id })}
            />
            <label>
              Wymagane decyzje
              <select
                value={n.policy ?? "ANY"}
                onChange={(e) => edit({ policy: e.target.value })}
              >
                <option value="ANY">Jedna akceptacja wystarcza</option>
                <option value="ALL">Wszyscy uczestnicy</option>
              </select>
            </label>
            {route("Po odrzuceniu", "rejected")}
            {route("Po terminie akceptacji", "timeout")}
          </>
        ) : null}
        {["APPROVAL", "WAIT_TASK", "WAIT_TIMER"].includes(n.type) ? (
          <label>
            Termin w sekundach od wejścia w krok
            <input
              type="number"
              required
              min={1}
              max={n.type === "APPROVAL" ? 7776000 : 31536000}
              value={n.seconds ?? 60}
              onChange={(e) => edit({ seconds: Number(e.target.value) })}
            />
          </label>
        ) : null}
        {n.type === "CREATE_TASK" ? (
          <>
            <label>
              Tytuł zadania
              <input
                required
                maxLength={200}
                value={n.title ?? ""}
                onChange={(e) => edit({ title: e.target.value })}
              />
            </label>
            <label>
              Instrukcja wewnętrzna
              <textarea
                maxLength={10000}
                value={n.instructions ?? ""}
                onChange={(e) => edit({ instructions: e.target.value })}
              />
            </label>
          </>
        ) : null}
        {n.type === "WAIT_TASK" ? (
          <>
            {route("Utworzone zadanie", "task_ref")}
            {route("Po terminie zadania", "timeout")}
          </>
        ) : null}
        {n.type === "UPDATE_RECORD" ? (
          <>
            <label>
              Stan docelowy
              <select
                value={n.state ?? "IN_PROGRESS"}
                onChange={(e) => edit({ state: e.target.value })}
              >
                <option value="IN_PROGRESS">W realizacji</option>
                <option value="FULFILLED">Zrealizowany</option>
                <option value="CLOSED">Zamknięty</option>
                <option value="CANCELLED">Anulowany</option>
              </select>
            </label>
            <label>
              Uzasadnienie zmiany
              <textarea
                required
                maxLength={10000}
                value={n.reason ?? ""}
                onChange={(e) => edit({ reason: e.target.value })}
              />
            </label>
          </>
        ) : null}
      </div>
      <button
        type="button"
        className="button button--secondary"
        onClick={remove}
      >
        Usuń krok {n.id}
      </button>
    </fieldset>
  );
}
