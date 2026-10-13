import { useEffect, useId, useRef, useState } from "react";
import type { Group, Role, TenantUser } from "../api/types";
import { useIncidentQuery } from "../incidents/hooks";
import { useRemotePage } from "./useRemotePage";

export type Choice = { id: string; label: string; disabled?: boolean };
export type ChoiceKind = "users" | "groups" | "roles";
export function RemoteCombobox({
  kind,
  label,
  value,
  onChange,
  disabled = false,
  allowInactive = false,
  clearable = true,
  endpoint,
  collection,
  cursor = "after_id",
}: {
  kind: ChoiceKind;
  label: string;
  value: Choice | null;
  onChange: (value: Choice | null) => void;
  disabled?: boolean;
  allowInactive?: boolean;
  clearable?: boolean;
  endpoint?: string;
  collection?: string;
  cursor?: string;
}) {
  const id = useId();
  const [open, setOpen] = useState(false);
  const [preview, setPreview] = useState(false);
  const [query, setQuery] = useState("");
  const [search, setSearch] = useState("");
  const [index, setIndex] = useState(-1);
  const root = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const timer = setTimeout(() => {
      setSearch(query);
      setIndex(-1);
    }, 250);
    return () => clearTimeout(timer);
  }, [query]);
  return (
    <div
      className="remote-combobox"
      ref={root}
      onBlur={(event) => {
        if (!root.current?.contains(event.relatedTarget)) setOpen(false);
      }}
    >
      <label htmlFor={id}>{label}</label>
      <div className="combobox-input">
        <input
          id={id}
          role="combobox"
          aria-autocomplete="list"
          aria-expanded={open}
          aria-controls={`${id}-list`}
          aria-activedescendant={
            open && index >= 0 ? `${id}-option-${index}` : undefined
          }
          autoComplete="off"
          maxLength={200}
          disabled={disabled}
          value={open ? query : (value?.label ?? "")}
          onFocus={() => {
            if (open) return;
            setOpen(true);
            setQuery("");
            setSearch("");
            setIndex(-1);
          }}
          onChange={(event) => {
            setOpen(true);
            setQuery(event.target.value);
            setIndex(-1);
          }}
          onClick={() => {
            if (!open) {
              setOpen(true);
              setQuery("");
              setSearch("");
              setIndex(-1);
            }
          }}
          onKeyDown={(event) => {
            if (event.key === "Escape") {
              setOpen(false);
              event.preventDefault();
            }
            if (event.key === "ArrowDown" || event.key === "ArrowUp") {
              setOpen(true);
              event.preventDefault();
            }
            if (event.key === "Enter" && open) event.preventDefault();
          }}
        />
        {value && clearable ? (
          <button
            type="button"
            className="button button--small button--secondary"
            disabled={disabled}
            aria-label={`Wyczyść: ${label}`}
            onClick={() => onChange(null)}
          >
            ×
          </button>
        ) : null}
      </div>
      {open && !disabled ? (
        <Options
          key={`${kind}:${endpoint ?? ""}`}
          kind={kind}
          endpoint={endpoint}
          collection={collection}
          cursor={cursor}
          allowInactive={allowInactive}
          search={search}
          pending={query !== search}
          id={id}
          index={index}
          setIndex={setIndex}
          choose={(choice) => {
            onChange(choice);
            setOpen(false);
            root.current?.querySelector("input")?.focus();
            setOpen(false);
          }}
        />
      ) : null}
      {value &&
        kind !== "roles" &&
        (!endpoint ||
          /\/lookups\/(users|groups)(\?|$)|\/fulfillment\/\d+\/users(\?|$)/.test(
            endpoint,
          )) && (
          <details
            className="reference-choice-preview"
            onToggle={(e) => setPreview(e.currentTarget.open)}
          >
            <summary>Podgląd: {label}</summary>
            {preview && (
              <ChoicePreview
                key={`${endpoint ?? kind}:${value.id}`}
                kind={kind}
                choiceID={value.id}
                endpoint={endpoint}
                collection={collection}
              />
            )}
          </details>
        )}
      {open && value ? (
        <small className="muted">Wybrano: {value.label}</small>
      ) : null}
    </div>
  );
}

function Options({
  kind,
  endpoint: customEndpoint,
  collection,
  cursor,
  allowInactive,
  search,
  pending,
  id,
  index,
  setIndex,
  choose,
}: {
  kind: ChoiceKind;
  endpoint?: string;
  collection?: string;
  cursor: string;
  allowInactive: boolean;
  search: string;
  pending: boolean;
  id: string;
  index: number;
  setIndex: (value: number) => void;
  choose: (value: Choice) => void;
}) {
  const endpoint =
    customEndpoint ??
    (kind === "users" ? "/api/v1/admin/users/" : `/api/v1/admin/rbac/${kind}`);
  const url = new URL(endpoint, window.location.origin);
  url.searchParams.set("limit", "50");
  url.searchParams.set("q", search);
  const page = useRemotePage<
    Role | Group | TenantUser | { id: string; title: string; number: string }
  >(`${url.pathname}${url.search}`, collection ?? kind, cursor);
  const options: Choice[] = page.items.map((item) =>
    "email" in item
      ? {
          id: item.id,
          label: item.email
            ? `${item.display_name} — ${item.email}`
            : item.display_name,
          disabled:
            !allowInactive && (!item.user_active || !item.membership_active),
        }
      : {
          id: item.id,
          label: "title" in item ? `${item.number} · ${item.title}` : item.name,
        },
  );
  const active = Math.min(index, options.length - 1);
  useEffect(() => {
    const input = document.getElementById(id);
    input?.setAttribute(
      "aria-activedescendant",
      active >= 0 ? `${id}-option-${active}` : "",
    );
    if (active < 0 || pending) input?.removeAttribute("aria-activedescendant");
    const handler = (event: KeyboardEvent) => {
      if (event.key === "Enter") event.preventDefault();
      if (event.key === "ArrowDown")
        setIndex(Math.min(options.length - 1, active + 1));
      if (event.key === "ArrowUp") setIndex(Math.max(0, active - 1));
      const option = options[active];
      if (
        event.key === "Enter" &&
        !pending &&
        !page.loading &&
        option &&
        !option.disabled
      )
        choose(option);
      if (event.key === "Home") {
        event.preventDefault();
        setIndex(0);
      }
      if (event.key === "End") {
        event.preventDefault();
        setIndex(options.length - 1);
      }
    };
    input?.addEventListener("keydown", handler);
    document
      .getElementById(`${id}-option-${active}`)
      ?.scrollIntoView?.({ block: "nearest" });
    return () => input?.removeEventListener("keydown", handler);
  }, [active, choose, id, options, page.loading, pending, setIndex]);
  return (
    <div className="combobox-popup">
      <ul
        id={`${id}-list`}
        role="listbox"
        aria-label="Wyniki wyszukiwania"
        aria-busy={page.loading || pending}
      >
        {!pending &&
          options.map((option, position) => (
            <li
              key={option.id}
              id={`${id}-option-${position}`}
              role="option"
              aria-selected={position === active}
              aria-disabled={option.disabled}
              className={
                position === active
                  ? "combobox-option is-active"
                  : "combobox-option"
              }
              onMouseDown={(event) => event.preventDefault()}
              onClick={() => {
                if (!option.disabled && !page.loading) choose(option);
              }}
            >
              {option.label}
              {option.disabled ? " (nieaktywne członkostwo)" : ""}
            </li>
          ))}
      </ul>
      {page.loading || pending ? <p role="status">Wyszukiwanie…</p> : null}
      {!page.loading && !pending && !options.length && !page.error ? (
        <p role="status">Brak wyników.</p>
      ) : null}
      {page.error ? (
        <div role="alert">
          {page.error}{" "}
          <button type="button" onClick={() => void page.retry()}>
            Spróbuj ponownie
          </button>
        </div>
      ) : null}
      {page.next ? (
        <button
          type="button"
          className="button button--secondary"
          disabled={page.loading || pending}
          onClick={() =>
            void page.more().then(() => document.getElementById(id)?.focus())
          }
        >
          Załaduj więcej wyników
        </button>
      ) : null}
    </div>
  );
}

type PreviewChoice = {
  id: string;
  name?: string;
  display_name?: string;
  user_active?: boolean;
  membership_active?: boolean;
  active?: boolean;
};
function ChoicePreview({
  kind,
  choiceID,
  endpoint,
  collection,
}: {
  kind: ChoiceKind;
  choiceID: string;
  endpoint?: string;
  collection?: string;
}) {
  const url = new URL(
    endpoint ?? `/api/v1/workspace/directory/${kind}/${choiceID}`,
    window.location.origin,
  );
  if (endpoint) {
    url.searchParams.set("id", choiceID);
    url.searchParams.set("limit", "1");
    url.searchParams.delete("q");
    url.searchParams.delete("after_id");
    url.searchParams.delete("before_id");
  }
  const q = useIncidentQuery<PreviewChoice | Record<string, PreviewChoice[]>>(
    `${url.pathname}${url.search}`,
  );
  if (q.loading) return <p role="status">Odczyt podglądu…</p>;
  if (q.error)
    return (
      <div role="alert">
        <p>{q.error}</p>
        <button type="button" onClick={q.reload}>
          Ponów odczyt podglądu
        </button>
      </div>
    );
  const item = endpoint
    ? (q.data as Record<string, PreviewChoice[]> | null)?.[
        collection ?? kind
      ]?.find((v) => v.id === choiceID)
    : (q.data as PreviewChoice | null);
  if (!item)
    return (
      <p role="status">
        Wybrany obiekt jest obecnie niedostępny w tym selektorze.
      </p>
    );
  const active = item.active ?? item.user_active ?? true,
    membership = item.membership_active ?? true;
  return (
    <dl>
      <dt>{kind === "users" ? "Użytkownik" : "Grupa"}</dt>
      <dd>{item.display_name ?? item.name}</dd>
      <dt>Stan w organizacji</dt>
      <dd>{active && membership ? "Aktywny" : "Nieaktywny"}</dd>
      <dt>Identyfikator</dt>
      <dd>{item.id}</dd>
    </dl>
  );
}
