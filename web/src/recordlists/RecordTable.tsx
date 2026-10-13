import { useEffect, useRef, useState, type ReactNode } from "react";
import { Link } from "react-router-dom";
import { useIncidentQuery } from "../incidents/hooks";
import type { ColumnConfig } from "./contracts";
export type RecordColumn<T> = {
  key: string;
  label: string;
  width: number;
  render: (record: T) => ReactNode;
};
export function RecordTable<T extends { id: string; number: string }>({
  records,
  columns,
  selected,
  selectionHeader,
  selectionCell,
  url,
  apiRoot,
  stateLabel = (state) => state,
}: {
  records: T[];
  columns: RecordColumn<T>[];
  selected: ColumnConfig[];
  selectionHeader?: ReactNode;
  selectionCell?: (r: T) => ReactNode;
  url: (r: T) => string;
  apiRoot: string;
  stateLabel?: (state: string) => string;
}) {
  const [preview, setPreview] = useState<T | null>(null);
  const visible = selected
    .map((c) => ({ config: c, column: columns.find((v) => v.key === c.key)! }))
    .filter((c) => c.column);
  return (
    <>
      <div className="table-scroll">
        <table className="record-table">
          <caption className="sr-only">
            Rekordy dostępne w bieżącej organizacji
          </caption>
          <thead>
            <tr>
              {selectionHeader && <th scope="col">{selectionHeader}</th>}
              {visible.map(({ config, column }) => (
                <th
                  key={column.key}
                  scope="col"
                  style={{ width: config.width, minWidth: config.width }}
                >
                  {column.label}
                </th>
              ))}
              <th scope="col">Akcje</th>
            </tr>
          </thead>
          <tbody>
            {records.map((r) => (
              <tr key={r.id}>
                {selectionCell && <td>{selectionCell(r)}</td>}
                {visible.map(({ column }) => (
                  <td key={column.key} className="wrap-cell">
                    {column.render(r)}
                  </td>
                ))}
                <td>
                  <details className="row-menu">
                    <summary aria-label={`Akcje ${r.number}`}>•••</summary>
                    <div>
                      <Link to={url(r)}>Otwórz rekord</Link>
                      <Link to={url(r)} target="_blank" rel="noopener">
                        Otwórz w nowej karcie
                      </Link>
                      <button
                        className="button button--small button--secondary"
                        onClick={() => setPreview(r)}
                      >
                        Szybki podgląd
                      </button>
                    </div>
                  </details>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {preview && (
        <Preview
          record={preview}
          path={`${apiRoot}/${preview.id}`}
          url={url(preview)}
          close={() => setPreview(null)}
          stateLabel={stateLabel}
        />
      )}
    </>
  );
}
function Preview({
  record,
  path,
  url,
  close,
  stateLabel,
}: {
  record: { number: string };
  path: string;
  url: string;
  close: () => void;
  stateLabel: (state: string) => string;
}) {
  const dialog = useRef<HTMLElement>(null);
  useEffect(() => {
    const previous = document.activeElement as HTMLElement | null;
    dialog.current?.querySelector<HTMLButtonElement>("button")?.focus();
    return () => previous?.focus();
  }, []);
  const query = useIncidentQuery<{
    number: string;
    title?: string;
    state: string;
    version: string;
    updated_at: string;
    assigned_name?: string;
    group_name?: string;
  }>(path);
  return (
    <div
      className="dialog-backdrop"
      onKeyDown={(e) => {
        if (e.key === "Escape") close();
        if (e.key === "Tab") {
          const items = Array.from(dialog.current?.querySelectorAll<HTMLElement>('button:not(:disabled),a[href]') ?? []);
          const first = items[0], last = items.at(-1);
          if (e.shiftKey && document.activeElement === first) { e.preventDefault(); last?.focus(); }
          if (!e.shiftKey && document.activeElement === last) { e.preventDefault(); first?.focus(); }
        }
      }}
    >
      <section
        ref={dialog}
        className="panel record-preview"
        role="dialog"
        aria-modal="true"
        aria-label={`Podgląd ${record.number}`}
      >
        <div className="panel-heading">
          <h2>Podgląd {record.number}</h2>
          <button
            className="button button--secondary"
            onClick={close}
          >
            Zamknij podgląd
          </button>
        </div>
        {query.loading ? (
          <p role="status">Ładowanie…</p>
        ) : query.error ? (
          <div role="alert">
            <p>{query.error}</p>
            <button className="button" onClick={query.reload}>
              Spróbuj ponownie
            </button>
          </div>
        ) : query.data ? (
          <>
            <h3>{query.data.title ?? query.data.number}</h3>
            <dl>
              <dt>Stan</dt>
              <dd>{stateLabel(query.data.state)}</dd>
              <dt>Wersja</dt>
              <dd>{query.data.version}</dd>
              <dt>Przypisanie</dt>
              <dd>
                {[query.data.group_name, query.data.assigned_name]
                  .filter(Boolean)
                  .join(" · ") || "Nieprzypisane"}
              </dd>
              <dt>Aktualizacja</dt>
              <dd>{new Date(query.data.updated_at).toLocaleString("pl-PL")}</dd>
            </dl>
            <Link to={url} onClick={close}>
              Otwórz pełny rekord
            </Link>
          </>
        ) : null}
      </section>
    </div>
  );
}
