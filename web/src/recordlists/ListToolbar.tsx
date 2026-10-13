import { useState } from "react";
import { useSearchParams } from "react-router-dom";
import { useAuth } from "../auth/AuthContext";
import { api, ApiError, download, jsonBody } from "../api/client";
import { RemoteCombobox, type Choice } from "../components/RemoteCombobox";
import { useRemotePage } from "../components/useRemotePage";
import { useCreation } from "../requests/useCreation";
import { FilterEditor, filterLabel } from "./FilterEditor";
import {
  columnsFrom,
  decodeFilter,
  type ColumnConfig,
  type FilterField,
  type ListConfig,
  type SavedView,
} from "./contracts";
const contextKeys = [
  "q",
  "view",
  "state",
  "category",
  "priority",
  "assigned_to",
  "group_id",
  "reporter_id",
  "from",
  "to",
  "active",
  "sla_risk",
];
export function ListToolbar({
  domain,
  fields,
  columns,
  sorts,
  groups,
  apiRoot,
  total,
  counts,
}: {
  domain: string;
  fields: FilterField[];
  columns: { key: string; label: string; width: number }[];
  sorts: { value: string; label: string }[];
  groups: { value: string; label: string }[];
  apiRoot: string;
  total?: string;
  counts?: { value: string; count: string }[];
}) {
  const [params, setParams] = useSearchParams();
  const { hasPermission } = useAuth();
  const defaults = columns.map(({ key, width }) => ({ key, width }));
  const selected = columnsFrom(params.get("columns"), defaults);
  const expression = decodeFilter(params.get("filter"));
  const views = useRemotePage<SavedView>(
    `/api/v1/record-views/?domain=${domain}`,
    "items",
    "before_id",
  );
  const creation = useCreation<SavedView>();
  const [chosen, setChosen] = useState<SavedView>();
  const [name, setName] = useState("");
  const [group, setGroup] = useState<Choice | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [conflict, setConflict] = useState<SavedView>();
  const [unresolved, setUnresolved] = useState(false);
  const [failedRead, setFailedRead] = useState(false);
  function change(values: Record<string, string | undefined>) {
    const next = new URLSearchParams(params);
    for (const key of [
      "after_id",
      "after_priority",
      "after_value",
      "before_id",
    ])
      next.delete(key);
    for (const [k, v] of Object.entries(values)) {
      if (v) next.set(k, v);
      else next.delete(k);
    }
    setParams(next);
  }
  const config: ListConfig = {
    filter: expression,
    sort: params.get("sort") ?? "newest",
    group_by: params.get("group_by") ?? undefined,
    columns: selected,
    context: Object.fromEntries(
      contextKeys.flatMap((k) => (params.get(k) ? [[k, params.get(k)!]] : [])),
    ),
  };
  function apply(view: SavedView) {
    const next = new URLSearchParams();
    for (const [k, v] of Object.entries(view.configuration.context ?? {}))
      next.set(k, v);
    if (view.configuration.filter)
      next.set("filter", JSON.stringify(view.configuration.filter));
    next.set("sort", view.configuration.sort);
    if (view.configuration.group_by)
      next.set("group_by", view.configuration.group_by);
    next.set("columns", JSON.stringify(view.configuration.columns));
    setParams(next);
    setChosen(view);
    setName(view.name);
    setGroup(
      view.group_id
        ? { id: view.group_id, label: `Grupa #${view.group_id}` }
        : null,
    );
    setConflict(undefined);
    setUnresolved(false);
    setFailedRead(false);
    setNotice(
      `Zastosowano widok „${view.name}”. Uprawnienia do rekordów pozostają sprawdzane na serwerze.`,
    );
  }
  async function readConflict() {
    if (!chosen) return;
    setBusy(true);
    try {
      setConflict(await api<SavedView>(`/api/v1/record-views/${chosen.id}`));
      setFailedRead(false);
    } catch (cause) {
      setError(
        cause instanceof Error ? cause.message : "Nie odczytano widoku.",
      );
      setFailedRead(true);
    } finally {
      setBusy(false);
    }
  }
  async function save(update: boolean) {
    if (busy || unresolved) return;
    setError("");
    setNotice("");
    const input = {
      domain,
      name: name.trim(),
      group_id: group?.id ?? null,
      configuration: config,
      version: update ? chosen?.version : "0",
    };
    if (!update) {
      const result = await creation.create("/api/v1/record-views/", input);
      if (result) {
        setChosen(result);
        setName(result.name);
        views.retry();
        setNotice("Widok został zapisany.");
      }
      return;
    }
    if (!chosen?.can_edit) return;
    setBusy(true);
    try {
      const result = await api<SavedView>(`/api/v1/record-views/${chosen.id}`, {
        method: "PUT",
        body: jsonBody(input),
      });
      setChosen(result);
      views.retry();
      setNotice("Widok został zaktualizowany.");
    } catch (cause) {
      setError(
        cause instanceof Error
          ? cause.message
          : "Nie potwierdzono aktualizacji widoku.",
      );
      if (cause instanceof ApiError && cause.status === 409) {
        setUnresolved(true);
        await readConflict();
      }
    } finally {
      setBusy(false);
    }
  }
  async function remove() {
    if (!chosen?.can_edit || busy || unresolved) return;
    setBusy(true);
    setError("");
    try {
      await api(`/api/v1/record-views/${chosen.id}?version=${chosen.version}`, {
        method: "DELETE",
      });
      setChosen(undefined);
      setName("");
      setGroup(null);
      views.retry();
      setNotice(
        "Widok usunięty. Rekordy i konfiguracja bieżącej listy pozostają dostępne.",
      );
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Nie usunięto widoku.");
      if (cause instanceof ApiError && cause.status === 409) {
        setUnresolved(true);
        await readConflict();
      }
    } finally {
      setBusy(false);
    }
  }
  async function exportCSV() {
    setBusy(true);
    setError("");
    try {
      const query = new URLSearchParams(params);
      for (const key of [
        "columns",
        "after_id",
        "after_priority",
        "after_value",
        "before_id",
        "queue",
      ])
        query.delete(key);
      query.set("format", "csv");
      await download(`${apiRoot}/?${query}`, `${domain}.csv`);
      setNotice("Eksport został pobrany.");
    } catch (cause) {
      setError(
        cause instanceof Error ? cause.message : "Eksport nie powiódł się.",
      );
    } finally {
      setBusy(false);
    }
  }
  const locked = busy || creation.busy || creation.uncertain;
  return (
    <section className="panel list-toolbar" aria-label="Konfiguracja listy">
      <div className="action-row">
        <strong>
          {total !== undefined
            ? `Dostępnych rekordów: ${total}`
            : "Licznik oczekuje na dane"}
        </strong>
        <label>
          Sortowanie
          <select
            aria-label="Sortowanie"
            value={config.sort}
            onChange={(e) => change({ sort: e.target.value })}
          >
            {sorts.map((s) => (
              <option key={s.value} value={s.value}>
                {s.label}
              </option>
            ))}
          </select>
        </label>
        <label>
          Grupowanie
          <select
            aria-label="Grupowanie"
            value={config.group_by ?? ""}
            onChange={(e) => change({ group_by: e.target.value })}
          >
            <option value="">Bez grupowania</option>
            {groups.map((g) => (
              <option key={g.value} value={g.value}>
                {g.label}
              </option>
            ))}
          </select>
        </label>
        <button
          className="button button--secondary"
          disabled={busy}
          onClick={() => void exportCSV()}
        >
          Eksport CSV
        </button>
      </div>
      {expression && (
        <p className="active-filter">
          <strong>Aktywny warunek:</strong> {filterLabel(expression, fields)}
        </p>
      )}
      <FilterEditor
        key={params.get("filter") ?? "none"}
        fields={fields}
        initial={expression}
        apply={(n) => change({ filter: n ? JSON.stringify(n) : undefined })}
      />
      {config.group_by && counts && (
        <div className="group-counts" aria-label="Grupy w całym zbiorze">
          {counts.map((g) => (
            <button
              key={g.value}
              className="button button--small button--secondary"
              onClick={() => {
                const condition = {
                  field: config.group_by,
                  operator: g.value === "" ? "empty" : "eq",
                  value: g.value,
                };
                change({
                  filter: JSON.stringify(
                    expression
                      ? { logic: "AND", children: [expression, condition] }
                      : condition,
                  ),
                  group_by: undefined,
                });
              }}
            >
              {fields
                .find((f) => f.key === config.group_by)
                ?.options?.find((o) => o.value === g.value)?.label ??
                (g.value || "Bez wartości")}
              : {g.count}
            </button>
          ))}
        </div>
      )}
      <details>
        <summary>Kolumny i zapisane widoki</summary>
        <fieldset disabled={locked}>
          <legend>Widoczne kolumny — kolejność i szerokość</legend>
          <div className="column-editor">
            {selected.map((c, i) => (
              <div key={c.key}>
                <strong>{columns.find((v) => v.key === c.key)?.label}</strong>
                <button
                  className="button button--small button--secondary"
                  disabled={i === 0}
                  aria-label={`Przesuń ${c.key} w lewo`}
                  onClick={() => {
                    const next = [...selected];
                    [next[i - 1], next[i]] = [next[i]!, next[i - 1]!];
                    change({ columns: JSON.stringify(next) });
                  }}
                >
                  ←
                </button>
                <button
                  className="button button--small button--secondary"
                  disabled={i === selected.length - 1}
                  aria-label={`Przesuń ${c.key} w prawo`}
                  onClick={() => {
                    const next = [...selected];
                    [next[i + 1], next[i]] = [next[i]!, next[i + 1]!];
                    change({ columns: JSON.stringify(next) });
                  }}
                >
                  →
                </button>
                <label>
                  Szerokość {columns.find((v) => v.key === c.key)?.label}
                  <input
                    type="range"
                    min={80}
                    max={600}
                    step={10}
                    value={c.width}
                    onChange={(e) =>
                      change({
                        columns: JSON.stringify(
                          selected.map((v) =>
                            v.key === c.key
                              ? { ...v, width: Number(e.target.value) }
                              : v,
                          ),
                        ),
                      })
                    }
                  />
                  <output>{c.width}px</output>
                </label>
                <button
                  className="button button--small button--secondary"
                  disabled={selected.length === 1}
                  onClick={() =>
                    change({
                      columns: JSON.stringify(
                        selected.filter((v) => v.key !== c.key),
                      ),
                    })
                  }
                >
                  Ukryj
                </button>
              </div>
            ))}
          </div>
          <label>
            Dodaj kolumnę
            <select
              value=""
              onChange={(e) => {
                const column = defaults.find((c) => c.key === e.target.value);
                if (column)
                  change({ columns: JSON.stringify([...selected, column]) });
              }}
            >
              <option value="">Wybierz…</option>
              {columns
                .filter((c) => !selected.some((v) => v.key === c.key))
                .map((c) => (
                  <option key={c.key} value={c.key}>
                    {c.label}
                  </option>
                ))}
            </select>
          </label>
        </fieldset>
        <div className="form-grid">
          <label>
            Zapisane widoki
            <select
              aria-label="Zapisane widoki"
              value={chosen?.id ?? ""}
              disabled={locked}
              onChange={(e) => {
                const found = views.items.find((v) => v.id === e.target.value);
                if (found) apply(found);
                else {
                  setChosen(undefined);
                  setName("");
                  setGroup(null);
                }
              }}
            >
              <option value="">Nowy widok</option>
              {views.items.map((v) => (
                <option key={v.id} value={v.id}>
                  {v.name}{" "}
                  {v.group_id ? `· grupa #${v.group_id}` : "· prywatny"}
                </option>
              ))}
            </select>
          </label>
          <label>
            Nazwa widoku
            <input
              value={name}
              maxLength={120}
              disabled={locked}
              onChange={(e) => setName(e.target.value)}
            />
          </label>
          {hasPermission("workspace.views.share") ? (
            <RemoteCombobox
              kind="groups"
              label="Udostępnij mojej grupie"
              endpoint="/api/v1/record-views/groups"
              value={group}
              onChange={setGroup}
              disabled={locked}
            />
          ) : (
            <p className="muted">Nowe widoki są prywatne.</p>
          )}
        </div>
        <p className="muted">
          Brak grupy oznacza widok prywatny. Udostępnienie konfiguracji nie
          nadaje dostępu do rekordów.
        </p>
        {views.error && (
          <div role="alert">
            {views.error}
            <button className="button" onClick={views.retry}>
              Pobierz widoki ponownie
            </button>
          </div>
        )}
        {views.next && (
          <button
            className="button button--secondary"
            disabled={views.loading}
            onClick={() => void views.more()}
          >
            Więcej zapisanych widoków
          </button>
        )}
        <div className="action-row">
          <button
            className="button"
            disabled={busy || creation.busy || !name.trim() || unresolved}
            onClick={() => void save(false)}
          >
            {creation.uncertain
              ? "Ponów niepotwierdzone tworzenie"
              : "Zapisz jako nowy widok"}
          </button>
          {chosen?.can_edit && (
            <>
              <button
                className="button button--secondary"
                disabled={locked || unresolved}
                onClick={() => void save(true)}
              >
                Aktualizuj widok
              </button>
              <button
                className="button button--secondary"
                disabled={locked || unresolved}
                onClick={() => void remove()}
              >
                Usuń zapisany widok
              </button>
            </>
          )}
        </div>
      </details>
      {(error || creation.error) && (
        <p role="alert">{error || creation.error}</p>
      )}
      {notice && <p role="status">{notice}</p>}
      {unresolved && (
        <section role="region" aria-label="Konflikt widoku">
          <h3>Konflikt konfiguracji widoku</h3>
          <p>Propozycja pozostaje w filtrach i kolumnach bieżącej listy.</p>
          <h4>Moja propozycja</h4>
          <pre>{JSON.stringify(config, null, 2)}</pre>
          {conflict && (
            <>
              <h4>Aktualna konfiguracja</h4>
              <pre>{JSON.stringify(conflict.configuration, null, 2)}</pre>
              <button
                className="button"
                onClick={() => {
                  setChosen(conflict);
                  setConflict(undefined);
                  setUnresolved(false);
                  setError("");
                }}
              >
                Zachowaj propozycję do ponownego zapisu
              </button>
              <button
                className="button button--secondary"
                onClick={() => apply(conflict)}
              >
                Użyj aktualnej konfiguracji
              </button>
            </>
          )}
          {failedRead && (
            <button
              className="button"
              disabled={busy}
              onClick={() => void readConflict()}
            >
              Pobierz aktualny widok
            </button>
          )}
        </section>
      )}
    </section>
  );
}
export function columnConfig(
  params: URLSearchParams,
  columns: { key: string; width: number }[],
): ColumnConfig[] {
  return columnsFrom(
    params.get("columns"),
    columns.map(({ key, width }) => ({ key, width })),
  );
}
