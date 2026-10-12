import { useEffect, useRef, useState } from "react";
import { flushSync } from "react-dom";
import { useSearchParams } from "react-router-dom";
import { api, ApiError, jsonBody } from "../api/client";
import { RemoteCombobox, type Choice } from "../components/RemoteCombobox";
import { useRemotePage } from "../components/useRemotePage";
import { useIncidentQuery, useIncidentTitleFocus } from "../incidents/hooks";
import { UnsavedGuard } from "../incidents/UnsavedGuard";
import { LoadState } from "../requests/LoadState";
import { useEditor } from "../requests/useEditor";
import { useCreation } from "../requests/useCreation";
import {
  message,
  type Condition,
  type Definition,
  type Field,
  type Item,
  type Node,
} from "../requests/contracts";
const base = "/api/v1/catalog/admin";
const blank: Definition = {
  title: "",
  description: "",
  fields: [],
  method: "MANUAL_TASKS",
  group_id: "0",
  steps: [{ title: "Realizacja usługi", instructions: "", required: true }],
};
export function CatalogAdminPage() {
  const heading = useIncidentTitleFocus();
  const [params, setParams] = useSearchParams();
  const id = params.get("id"),
    [create, setCreate] = useState<Definition | null>(null);
  const [nodeDirty, setNodeDirty] = useState(false);
  const items = useRemotePage<Item>(
    `${base}/items?limit=25&q=${encodeURIComponent(params.get("q") ?? "")}`,
    "items",
  );
  const templates = useIncidentQuery<{ templates: Definition[] }>(
    `${base}/templates`,
  );
  const detail = useIncidentQuery<Item>(id ? `${base}/items/${id}` : null);
  function select(id?: string) {
    setCreate(null);
    const p = new URLSearchParams(params);
    if (id) p.set("id", id);
    else p.delete("id");
    setParams(p);
  }
  return (
    <>
      <h1 ref={heading} tabIndex={-1}>
        Administracja katalogiem
      </h1>
      <p>
        Zmiany szkicu nie zmieniają bieżącej publikacji. Publikuj świadomie po
        wskazaniu grupy i zadań realizacji.
      </p>
      {!id && !create ? (
        <>
          <div className="action-row">
            <button
              className="button button--primary"
              disabled={nodeDirty}
              onClick={() => setCreate(structuredClone(blank))}
            >
              Nowa usługa
            </button>
            {templates.data?.templates.map((t) => (
              <button
                key={t.title}
                className="button button--secondary"
                disabled={nodeDirty}
                onClick={() => setCreate(structuredClone(t))}
              >
                Szablon: {t.title}
              </button>
            ))}
          </div>
          <LoadState
            loading={templates.loading}
            error={templates.error}
            retry={templates.reload}
          />
          <Nodes onEditing={setNodeDirty} />
          <label>
            Wyszukaj szkic
            <input
              value={params.get("q") ?? ""}
              maxLength={200}
              onChange={(e) => {
                const p = new URLSearchParams(params);
                p.set("q", e.target.value);
                setParams(p, { replace: true });
              }}
            />
          </label>
          <LoadState
            loading={items.loading}
            error={items.error}
            retry={items.retry}
          />
          <div className="table-scroll">
            <table>
              <thead>
                <tr>
                  <th>Usługa</th>
                  <th>Szkic</th>
                  <th>Publikacja</th>
                  <th>Akcja</th>
                </tr>
              </thead>
              <tbody>
                {items.items.map((i) => (
                  <tr key={i.id}>
                    <td>{i.draft.title}</td>
                    <td>{i.version}</td>
                    <td>
                      {i.published_version
                        ? `Wersja ${i.published_version}`
                        : "Nieopublikowana"}
                    </td>
                    <td>
                      <button
                        className="button button--secondary"
                        onClick={() => select(i.id)}
                      >
                        Edytuj {i.draft.title}
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          {!items.loading && !items.error && !items.items.length ? (
            <p>Brak usług. Utwórz katalog, kategorię i pierwszą usługę.</p>
          ) : null}
          {items.next ? (
            <button
              className="button button--secondary"
              onClick={() => void items.more()}
            >
              Więcej usług
            </button>
          ) : null}
        </>
      ) : null}
      {id ? (
        <LoadState
          loading={detail.loading}
          error={detail.error}
          retry={detail.reload}
        />
      ) : null}
      {create || detail.data ? (
        <ItemEditor
          key={id ?? "new"}
          initial={
            create
              ? { id: "", category_id: "", version: "0", draft: create }
              : detail.data!
          }
          saved={(i) => {
            select(i.id);
            items.retry();
            detail.reload();
          }}
          back={() => select()}
        />
      ) : null}
    </>
  );
}
function Nodes({ onEditing }: { onEditing: (dirty: boolean) => void }) {
  const catalogs = useRemotePage<Node>(`${base}/catalogs?limit=100`, "items"),
    categories = useRemotePage<Node>(`${base}/categories?limit=100`, "items");
  const [editing, setEditing] = useState<{ kind: string; node: Node }>(),
    [busy, setBusy] = useState(false),
    [error, setError] = useState<string>();
  const creation = useCreation<Node>();
  const [conflict, setConflict] = useState<Node>();
  const controller = useRef<AbortController | undefined>(undefined),
    alive = useRef(true);
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
      controller.current?.abort();
    };
  }, []);
  useEffect(() => {
    onEditing(!!editing);
    return () => onEditing(false);
  }, [editing, onEditing]);
  async function save() {
    if (!editing || busy || conflict) return;
    setBusy(true);
    setError(undefined);
    const c = new AbortController();
    controller.current = c;
    try {
      if (!editing.node.id) {
        const result = await creation.create(`${base}/${editing.kind}`, {
          name: editing.node.name,
          description: editing.node.description,
          version: editing.node.version,
          ...(editing.kind === "categories"
            ? { catalog_id: editing.node.catalog_id }
            : {}),
        });
        if (result) {
          setEditing(undefined);
          catalogs.retry();
          categories.retry();
        }
        return;
      }
      await api<Node>(
        `${base}/${editing.kind}${editing.node.id ? `/${editing.node.id}` : ""}`,
        {
          method: editing.node.id ? "PUT" : "POST",
          body: jsonBody({
            name: editing.node.name,
            description: editing.node.description,
            version: editing.node.version,
            ...(editing.kind === "categories"
              ? { catalog_id: editing.node.catalog_id }
              : {}),
          }),
          signal: c.signal,
        },
      );
      if (!alive.current || c.signal.aborted) return;
      setEditing(undefined);
      catalogs.retry();
      categories.retry();
    } catch (e) {
      if (!alive.current || c.signal.aborted) return;
      setError(message(e));
      if (e instanceof ApiError && e.status === 409) {
        try {
          const current = await api<Node>(
            `${base}/${editing.kind}/${editing.node.id}`,
            { signal: c.signal },
          );
          if (alive.current) setConflict(current);
        } catch (cause) {
          if (alive.current) setError(`${message(e)} ${message(cause)}`);
        }
      }
    } finally {
      if (alive.current) setBusy(false);
    }
  }
  return (
    <section className="panel">
      <UnsavedGuard dirty={!!editing} includeSearch />
      <h2>Katalogi i kategorie</h2>
      <div className="action-row">
        {["catalogs", "categories"].map((kind) => (
          <button
            key={kind}
            className="button button--secondary"
            disabled={!!editing}
            onClick={() =>
              setEditing({
                kind,
                node: {
                  id: "",
                  name: "",
                  description: "",
                  version: "0",
                  catalog_id: "",
                },
              })
            }
          >
            {kind === "catalogs" ? "Nowy katalog" : "Nowa kategoria"}
          </button>
        ))}
      </div>
      <LoadState
        loading={catalogs.loading || categories.loading}
        error={catalogs.error || categories.error}
        retry={() => {
          catalogs.retry();
          categories.retry();
        }}
      />
      {[
        ...catalogs.items.map((n) => ({ kind: "catalogs", node: n })),
        ...categories.items.map((n) => ({ kind: "categories", node: n })),
      ].map(({ kind, node }) => (
        <p key={`${kind}:${node.id}`}>
          {kind === "catalogs" ? "Katalog" : "Kategoria"}: {node.name}{" "}
          <button
            className="button button--small button--secondary"
            disabled={!!editing}
            onClick={() => setEditing({ kind, node: { ...node } })}
          >
            Zmień {node.name}
          </button>
        </p>
      ))}
      {catalogs.next ? (
        <button
          className="button button--secondary"
          onClick={() => void catalogs.more()}
        >
          Więcej katalogów
        </button>
      ) : null}
      {categories.next ? (
        <button
          className="button button--secondary"
          onClick={() => void categories.more()}
        >
          Więcej kategorii
        </button>
      ) : null}
      {editing ? (
        <form
          onSubmit={(e) => {
            e.preventDefault();
            void save();
          }}
        >
          <fieldset
            disabled={busy || creation.busy || creation.uncertain || !!conflict}
          >
            <label>
              Nazwa
              <input
                required
                maxLength={160}
                value={editing.node.name}
                onChange={(e) =>
                  setEditing({
                    ...editing,
                    node: { ...editing.node, name: e.target.value },
                  })
                }
              />
            </label>
            <label>
              Opis
              <textarea
                maxLength={10000}
                value={editing.node.description}
                onChange={(e) =>
                  setEditing({
                    ...editing,
                    node: { ...editing.node, description: e.target.value },
                  })
                }
              />
            </label>
            {editing.kind === "categories" ? (
              <label>
                Katalog
                <select
                  aria-label="Katalog"
                  required
                  disabled={!!editing.node.id}
                  value={editing.node.catalog_id ?? ""}
                  onChange={(e) =>
                    setEditing({
                      ...editing,
                      node: { ...editing.node, catalog_id: e.target.value },
                    })
                  }
                >
                  <option value="">Wybierz…</option>
                  {catalogs.items.map((n) => (
                    <option key={n.id} value={n.id}>
                      {n.name}
                    </option>
                  ))}
                </select>
              </label>
            ) : null}
          </fieldset>
          {error || creation.error ? (
            <p role="alert">{error ?? creation.error}</p>
          ) : null}
          {conflict ? (
            <section className="conflict-panel">
              <h3>Konflikt wersji nazwy i opisu</h3>
              <p>
                Twoja propozycja: {editing.node.name},{" "}
                {editing.node.description}. Aktualny stan: {conflict.name},{" "}
                {conflict.description}, wersja {conflict.version}.
              </p>
              <button
                type="button"
                className="button button--secondary"
                onClick={() => {
                  setEditing({
                    ...editing,
                    node: { ...editing.node, version: conflict.version },
                  });
                  setConflict(undefined);
                  setError(undefined);
                }}
              >
                Zachowaj propozycję na aktualnej wersji
              </button>
            </section>
          ) : null}
          {creation.uncertain ? (
            <p>
              Nie potwierdzono zapisu. Ponów identyczne utworzenie; klucz
              zapobiega duplikatom.
            </p>
          ) : null}
          <div className="action-row">
            <button
              className="button button--primary"
              disabled={busy || creation.busy || !!conflict}
            >
              Zapisz nazwę i opis
            </button>
            <button
              type="button"
              className="button button--secondary"
              disabled={busy || creation.busy || creation.uncertain}
              onClick={() => {
                setEditing(undefined);
                setConflict(undefined);
                setError(undefined);
              }}
            >
              Odrzuć edycję
            </button>
          </div>
        </form>
      ) : null}
    </section>
  );
}
function ItemEditor({
  initial,
  saved,
  back,
}: {
  initial: Item;
  saved: (i: Item) => void;
  back: () => void;
}) {
  const editor = useEditor(initial, `${base}/items/${initial.id}`),
    [draft, setDraft] = useState(() => structuredClone(initial.draft)),
    [category, setCategory] = useState(initial.category_id),
    [dirty, setDirty] = useState(false),
    [group, setGroup] = useState<Choice | null>(
      initial.draft.group_id !== "0"
        ? {
            id: initial.draft.group_id,
            label: `Grupa #${initial.draft.group_id}`,
          }
        : null,
    ),
    [audience, setAudience] = useState<Choice | null>(
      initial.draft.audience_group_id
        ? {
            id: initial.draft.audience_group_id,
            label: `Grupa #${initial.draft.audience_group_id}`,
          }
        : null,
    );
  const categories = useRemotePage<Node>(
    `${base}/categories?limit=100`,
    "items",
  );
  const [error, setError] = useState<string>();
  const creation = useCreation<Item>();
  const edit = (patch: Partial<Definition>) => {
    setDraft({ ...draft, ...patch });
    setDirty(true);
  };
  async function save() {
    setError(undefined);
    if (!initial.id) {
      const item = await creation.create(`${base}/items`, {
        category_id: category,
        version: "0",
        draft,
      });
      if (item) {
        flushSync(() => setDirty(false));
        saved(item);
      }
      return;
    }
    const ok = await editor.save(
      `${base}/items/${initial.id}`,
      { category_id: category, version: editor.record.version, draft },
      "PUT",
    );
    if (ok) setDirty(false);
  }
  async function publish(withdraw = false) {
    const ok = await editor.save(
      `${base}/items/${initial.id}/${withdraw ? "withdraw" : "publish"}`,
      { version: editor.record.version },
    );
    if (ok) saved(editor.record);
  }
  const disabled = editor.busy || creation.busy || !!editor.conflict;
  return (
    <>
      <UnsavedGuard dirty={dirty} includeSearch />
      <button className="button button--secondary" onClick={back}>
        Lista usług
      </button>
      <form
        className="panel"
        onSubmit={(e) => {
          e.preventDefault();
          void save();
        }}
      >
        <h2>{initial.id ? "Szkic usługi" : "Nowa usługa"}</h2>
        <p>
          Wersja szkicu {editor.record.version}; publikacja{" "}
          {editor.record.published_version ?? "brak"}.
        </p>
        <fieldset disabled={disabled || creation.uncertain}>
          <label>
            Kategoria
            <select
              aria-label="Kategoria"
              required
              value={category}
              disabled={!!initial.id}
              onChange={(e) => {
                setCategory(e.target.value);
                setDirty(true);
              }}
            >
              <option value="">Wybierz…</option>
              {categories.items.map((n) => (
                <option key={n.id} value={n.id}>
                  {n.name}
                </option>
              ))}
            </select>
          </label>
          {categories.next ? (
            <button
              type="button"
              className="button button--secondary"
              onClick={() => void categories.more()}
            >
              Więcej kategorii
            </button>
          ) : null}
          <LoadState
            loading={categories.loading}
            error={categories.error}
            retry={categories.retry}
          />
          <label>
            Nazwa usługi
            <input
              required
              maxLength={200}
              value={draft.title}
              onChange={(e) => edit({ title: e.target.value })}
            />
          </label>
          <label>
            Opis usługi
            <textarea
              maxLength={10000}
              value={draft.description}
              onChange={(e) => edit({ description: e.target.value })}
            />
          </label>
          <RemoteCombobox
            kind="groups"
            label="Grupa realizująca"
            endpoint="/api/v1/catalog/lookups/groups"
            value={group}
            onChange={(g) => {
              setGroup(g);
              edit({ group_id: g?.id ?? "0" });
            }}
          />
          <RemoteCombobox
            kind="groups"
            label="Ogranicz portal do grupy (puste: wszyscy w organizacji)"
            endpoint="/api/v1/catalog/lookups/groups"
            value={audience}
            onChange={(g) => {
              setAudience(g);
              edit({ audience_group_id: g?.id });
            }}
          />
          <h3>Formularz</h3>
          <p>
            Warunki wskazują wyłącznie wcześniejsze pola. Ukryte wartości nie są
            wysyłane.
          </p>
          {draft.fields.map((f, index) => (
            <FieldEditor
              key={index}
              field={f}
              previous={draft.fields.slice(0, index)}
              update={(next) =>
                edit({
                  fields: draft.fields.map((v, i) => (i === index ? next : v)),
                })
              }
              remove={() =>
                edit({ fields: draft.fields.filter((_, i) => i !== index) })
              }
            />
          ))}
          <button
            type="button"
            className="button button--secondary"
            disabled={draft.fields.length >= 50}
            onClick={() =>
              edit({
                fields: [
                  ...draft.fields,
                  {
                    key: `field_${draft.fields.length + 1}`,
                    label: "Nowe pole",
                    type: "text",
                    required: false,
                  },
                ],
              })
            }
          >
            Dodaj pole
          </button>
          <h3>Jawna realizacja przez zadania</h3>
          <p>
            Każde zadanie trafia do wybranej grupy. Co najmniej jedno musi być
            wymagane.
          </p>
          {draft.steps.map((s, index) => (
            <fieldset key={index}>
              <legend>Zadanie {index + 1}</legend>
              <label>
                Tytuł zadania
                <input
                  required
                  maxLength={200}
                  value={s.title}
                  onChange={(e) =>
                    edit({
                      steps: draft.steps.map((v, i) =>
                        i === index ? { ...v, title: e.target.value } : v,
                      ),
                    })
                  }
                />
              </label>
              <label>
                Instrukcja wewnętrzna
                <textarea
                  maxLength={10000}
                  value={s.instructions}
                  onChange={(e) =>
                    edit({
                      steps: draft.steps.map((v, i) =>
                        i === index
                          ? { ...v, instructions: e.target.value }
                          : v,
                      ),
                    })
                  }
                />
              </label>
              <label>
                <input
                  type="checkbox"
                  checked={s.required}
                  onChange={(e) =>
                    edit({
                      steps: draft.steps.map((v, i) =>
                        i === index ? { ...v, required: e.target.checked } : v,
                      ),
                    })
                  }
                />{" "}
                Wymagane przed zamknięciem
              </label>
              <button
                type="button"
                className="button button--secondary"
                onClick={() =>
                  edit({ steps: draft.steps.filter((_, i) => i !== index) })
                }
              >
                Usuń zadanie {index + 1}
              </button>
            </fieldset>
          ))}
          <button
            type="button"
            className="button button--secondary"
            disabled={draft.steps.length >= 20}
            onClick={() =>
              edit({
                steps: [
                  ...draft.steps,
                  { title: "", instructions: "", required: true },
                ],
              })
            }
          >
            Dodaj zadanie
          </button>
        </fieldset>
        <LoadState error={error ?? editor.error ?? creation.error} />
        {creation.uncertain ? (
          <p>
            Nie potwierdzono zapisu. Ponów identyczne utworzenie; klucz
            zapobiega duplikatom.
          </p>
        ) : null}
        {editor.notice ? <p role="status">{editor.notice}</p> : null}
        {editor.conflict ? (
          <section className="conflict-panel">
            <h3>Konflikt wersji</h3>
            <p>
              Twoje pola pozostają w formularzu. Aktualny szkic ma wersję{" "}
              {editor.conflict.version}.
            </p>
            <details>
              <summary>Aktualny szkic</summary>
              <pre>{JSON.stringify(editor.conflict.draft, null, 2)}</pre>
            </details>
            <button
              type="button"
              className="button button--secondary"
              onClick={editor.resolve}
            >
              Zachowaj propozycję, zapisz na aktualnej wersji
            </button>
          </section>
        ) : null}
        <div className="action-row">
          <button className="button button--primary" disabled={disabled}>
            Zapisz szkic
          </button>
          {initial.id ? (
            <>
              <button
                type="button"
                className="button button--secondary"
                disabled={disabled || dirty}
                onClick={() => void publish()}
              >
                Opublikuj zapisaną wersję
              </button>
              {editor.record.published_version ? (
                <button
                  type="button"
                  className="button button--secondary"
                  disabled={disabled}
                  onClick={() => void publish(true)}
                >
                  Wycofaj z portalu
                </button>
              ) : null}
            </>
          ) : null}
        </div>
      </form>
    </>
  );
}
function FieldEditor({
  field: f,
  previous,
  update,
  remove,
}: {
  field: Field;
  previous: Field[];
  update: (f: Field) => void;
  remove: () => void;
}) {
  return (
    <fieldset>
      <legend>Pole {f.key}</legend>
      <div className="form-grid">
        <label>
          Klucz pola
          <input
            required
            pattern="[a-z][a-z0-9_]{0,47}"
            value={f.key}
            onChange={(e) => update({ ...f, key: e.target.value })}
          />
        </label>
        <label>
          Etykieta
          <input
            required
            maxLength={160}
            value={f.label}
            onChange={(e) => update({ ...f, label: e.target.value })}
          />
        </label>
        <label>
          Typ
          <select
            value={f.type}
            onChange={(e) =>
              update({
                ...f,
                type: e.target.value as Field["type"],
                options: e.target.value === "select" ? ["Opcja 1"] : undefined,
              })
            }
          >
            {Object.entries({
              text: "Tekst",
              longtext: "Długi tekst",
              number: "Liczba",
              date: "Data",
              checkbox: "Checkbox",
              select: "Lista wyboru",
            }).map(([v, l]) => (
              <option key={v} value={v}>
                {l}
              </option>
            ))}
          </select>
        </label>
        <label>
          <input
            type="checkbox"
            checked={f.required}
            onChange={(e) => update({ ...f, required: e.target.checked })}
          />{" "}
          Wymagane
        </label>
      </div>
      {f.type === "select" ? (
        <label>
          Opcje (jedna w wierszu)
          <textarea
            value={f.options?.join("\n") ?? ""}
            onChange={(e) =>
              update({ ...f, options: e.target.value.split("\n") })
            }
          />
        </label>
      ) : null}
      <Rule
        label="Widoczne, gdy"
        condition={f.visible_when}
        previous={previous}
        update={(c) => update({ ...f, visible_when: c })}
      />
      <Rule
        label="Wymagane, gdy"
        condition={f.required_when}
        previous={previous}
        update={(c) => update({ ...f, required_when: c })}
      />
      <button
        type="button"
        className="button button--secondary"
        onClick={remove}
      >
        Usuń pole {f.key}
      </button>
    </fieldset>
  );
}
function Rule({
  label,
  condition,
  previous,
  update,
}: {
  label: string;
  condition?: Condition;
  previous: Field[];
  update: (c: Condition | undefined) => void;
}) {
  const parent = previous.find((f) => f.key === condition?.field);
  return (
    <div className="form-grid">
      <label>
        {label}
        <select
          value={condition?.field ?? ""}
          onChange={(e) => {
            const p = previous.find((f) => f.key === e.target.value);
            update(
              p
                ? {
                    field: p.key,
                    equals:
                      p.type === "checkbox"
                        ? true
                        : p.type === "number"
                          ? 0
                          : (p.options?.[0] ?? ""),
                  }
                : undefined,
            );
          }}
        >
          <option value="">Bez warunku</option>
          {previous.map((p) => (
            <option key={p.key} value={p.key}>
              {p.label} ({p.key})
            </option>
          ))}
        </select>
      </label>
      {parent && condition ? (
        <label>
          Wartość warunku
          {parent.type === "checkbox" ? (
            <select
              value={String(condition.equals)}
              onChange={(e) =>
                update({ ...condition, equals: e.target.value === "true" })
              }
            >
              <option value="true">Tak</option>
              <option value="false">Nie</option>
            </select>
          ) : parent.type === "select" ? (
            <select
              value={String(condition.equals)}
              onChange={(e) => update({ ...condition, equals: e.target.value })}
            >
              {parent.options?.map((o) => (
                <option key={o}>{o}</option>
              ))}
            </select>
          ) : (
            <input
              type={
                parent.type === "number"
                  ? "number"
                  : parent.type === "date"
                    ? "date"
                    : "text"
              }
              value={String(condition.equals)}
              onChange={(e) =>
                update({
                  ...condition,
                  equals:
                    parent.type === "number"
                      ? Number(e.target.value)
                      : e.target.value,
                })
              }
            />
          )}
        </label>
      ) : null}
    </div>
  );
}
