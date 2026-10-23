import { useEffect, useId, useRef, useState } from "react";
import { api, jsonBody } from "../api/client";
import { useRemotePage } from "./useRemotePage";

export type InventoryCatalogKind = "manufacturers" | "models" | "locations";
type CatalogValue = { id: string; kind: string; value: string; manufacturer?: string };

const labels: Record<InventoryCatalogKind, string> = {
  manufacturers: "Producent",
  models: "Model",
  locations: "Lokalizacja",
};

export function InventoryCatalogCombobox({
  kind,
  value,
  onChange,
  manufacturer = "",
  disabled = false,
}: {
  kind: InventoryCatalogKind;
  value: string;
  onChange: (value: string) => void;
  manufacturer?: string;
  disabled?: boolean;
}) {
  const id = useId();
  const root = useRef<HTMLDivElement>(null);
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [search, setSearch] = useState("");
  const [active, setActive] = useState(-1);
  const [saving, setSaving] = useState(false);
  const [createError, setCreateError] = useState("");

  useEffect(() => {
    const timer = setTimeout(() => {
      setSearch(query.trim());
      setActive(-1);
    }, 200);
    return () => clearTimeout(timer);
  }, [query]);

  const params = new URLSearchParams({ q: search, limit: "25" });
  if (kind === "models" && manufacturer) params.set("manufacturer", manufacturer);
  const catalogPath = `/api/v1/cmdb/lookups/assets/${kind}`;
  const endpoint = `${catalogPath}?${params}`;
  const page = useRemotePage<CatalogValue>(endpoint, "items", "before_id", open);
  const cleanQuery = query.trim();
  const existing = page.items.some(
    (item) => item.value.toLocaleLowerCase("pl-PL") === cleanQuery.toLocaleLowerCase("pl-PL"),
  );
  const canCreate = Boolean(cleanQuery) && !existing && !page.loading && !page.error && !saving;

  function choose(item: CatalogValue) {
    onChange(item.value);
    setQuery("");
    setCreateError("");
    setOpen(false);
    root.current?.querySelector("input")?.focus();
  }

  async function createValue() {
    if (!canCreate) return;
    setSaving(true);
    setCreateError("");
    try {
      const created = await api<CatalogValue>(catalogPath, {
        method: "POST",
        body: jsonBody({
          value: cleanQuery,
          ...(kind === "models" ? { manufacturer } : {}),
        }),
      });
      choose(created);
    } catch (cause) {
      setCreateError(cause instanceof Error ? cause.message : "Nie udało się zapisać wartości.");
    } finally {
      setSaving(false);
    }
  }

  return (
    <div
      className="remote-combobox"
      ref={root}
      onBlur={(event) => {
        if (!root.current?.contains(event.relatedTarget)) setOpen(false);
      }}
    >
      <label htmlFor={id}>{labels[kind]}</label>
      <div className="combobox-input">
        <input
          id={id}
          role="combobox"
          aria-autocomplete="list"
          aria-expanded={open}
          aria-controls={`${id}-list`}
          aria-activedescendant={open && active >= 0 && page.items[active] ? `${id}-option-${active}` : undefined}
          autoComplete="off"
          maxLength={160}
          disabled={disabled}
          value={open ? query : value}
          onFocus={() => {
            setOpen(true);
            setQuery("");
            setSearch("");
            setActive(-1);
            setCreateError("");
          }}
          onChange={(event) => {
            const next = event.target.value;
            onChange("");
            setQuery(next);
            setActive(-1);
            setOpen(true);
            setCreateError("");
          }}
          onKeyDown={(event) => {
            if (event.key === "Escape") {
              setOpen(false);
              event.preventDefault();
            } else if (event.key === "ArrowDown") {
              setOpen(true);
              setActive((index) => Math.min(page.items.length - 1, index + 1));
              event.preventDefault();
            } else if (event.key === "ArrowUp") {
              setActive((index) => Math.max(0, index - 1));
              event.preventDefault();
            } else if (event.key === "Enter" && open) {
              event.preventDefault();
              if (!page.loading && active >= 0 && page.items[active]) choose(page.items[active]);
              else if (canCreate) void createValue();
            }
          }}
        />
        {value && !open && (
          <button
            type="button"
            className="button button--small button--secondary"
            disabled={disabled}
            aria-label={`Wyczyść: ${labels[kind]}`}
            onClick={() => onChange("")}
          >
            ×
          </button>
        )}
      </div>
      {open && !disabled && (
        <div className="combobox-popup">
          <ul id={`${id}-list`} role="listbox" aria-label={`${labels[kind]} — zapisane wartości`} aria-busy={page.loading}>
            {page.items.map((item, index) => (
              <li
                key={item.id}
                id={`${id}-option-${index}`}
                role="option"
                aria-selected={index === active}
                className={index === active ? "combobox-option is-active" : "combobox-option"}
                onMouseDown={(event) => event.preventDefault()}
                onClick={() => choose(item)}
              >
                {item.value}
                {item.manufacturer && <small className="ml-2 text-slate-500">{item.manufacturer}</small>}
              </li>
            ))}
          </ul>
          {page.loading && <p role="status">Wyszukiwanie…</p>}
          {!page.loading && !page.error && page.items.length === 0 && <p role="status">Brak zapisanych wartości.</p>}
          {page.error && (
            <div role="alert">
              {page.error}{" "}
              <button type="button" onClick={() => void page.retry()}>Spróbuj ponownie</button>
            </div>
          )}
          {page.next && (
            <button
              type="button"
              className="button button--secondary"
              disabled={page.loading}
              onMouseDown={(event) => event.preventDefault()}
              onClick={() => void page.more()}
            >
              Załaduj więcej wartości
            </button>
          )}
          {canCreate && (
            <button
              type="button"
              className="combobox-option border-t border-slate-200 font-semibold text-sn-teal"
              disabled={saving}
              onMouseDown={(event) => event.preventDefault()}
              onClick={() => void createValue()}
            >
              Dodaj „{cleanQuery}” do listy
            </button>
          )}
          {saving && <p role="status">Zapisywanie wartości…</p>}
          {createError && <p role="alert">{createError}</p>}
        </div>
      )}
      {value && <small className="muted">Wybrano: {value}</small>}
    </div>
  );
}
