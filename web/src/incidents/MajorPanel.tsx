import {
  OrganizationDateInput,
  dateInputValue,
  parseDateInput,
} from "../metadata/dateInput";
import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type FormEvent,
} from "react";
import { Link } from "react-router-dom";
import { api, ApiError } from "../api/client";
import { useAuth } from "../auth/AuthContext";
import { RemoteCombobox, type Choice } from "../components/RemoteCombobox";
import { useCMDBLocale } from "../cmdb/messages";
import { formatDateTime } from "../metadata/format";
import type { Incident } from "./contracts";
import type {
  MajorAction,
  MajorCase,
  MajorView,
  MajorUpdate,
  MajorDecision,
} from "./majorContracts";
import { majorMessages } from "./majorMessages";

export const localInput = dateInputValue;
const utc = parseDateInput;
const person = (id?: string, name?: string): Choice | null =>
  id ? { id, label: name || id } : null;
type CoordinationDraft = ReturnType<typeof coordinationOf>;
function coordinationOf(c?: MajorCase) {
  return {
    commander: person(
      c?.commander_user_id,
      c?.people?.[c.commander_user_id ?? ""],
    ),
    deputy: person(c?.deputy_user_id, c?.people?.[c.deputy_user_id ?? ""]),
    owner: person(
      c?.communications_owner_id,
      c?.people?.[c.communications_owner_id ?? ""],
    ),
    bridge: c?.bridge_url ?? "",
    impact: c?.impact_summary ?? "",
    confidence: c?.impact_confidence ?? "ESTIMATED",
    workaround: c?.workaround ?? "",
    audience: c?.communication_audience ?? "ALL_LINKED",
    interval: c?.update_interval_minutes ?? 60,
    next: localInput(c?.next_update_at),
    detected: localInput(c?.detected_at),
    teams: (c?.teams ?? []).map((v) => ({
      id: v.group_id,
      label: v.group_name,
    })),
    services: (c?.links ?? [])
      .filter((v) => v.type === "SERVICE")
      .map((v) => ({ id: v.id, label: v.name })),
    cis: (c?.links ?? [])
      .filter((v) => v.type === "CI")
      .map((v) => ({ id: v.id, label: v.name })),
  };
}

export function ReferenceList({
  label,
  value,
  onChange,
  endpoint,
  collection = "items",
  cursor = "before_id",
  disabled = false,
}: {
  label: string;
  value: Choice[];
  onChange: (value: Choice[]) => void;
  endpoint: string;
  collection?: string;
  cursor?: string;
  disabled?: boolean;
}) {
  const locale = useCMDBLocale(),
    t = majorMessages[locale];
  return (
    <div>
      <RemoteCombobox
        kind="users"
        label={label}
        value={null}
        onChange={(v) => {
          if (v && !value.some((x) => x.id === v.id)) onChange([...value, v]);
        }}
        endpoint={endpoint}
        collection={collection}
        cursor={cursor}
        disabled={disabled}
      />
      <ul className="module08-chips">
        {value.map((v) => (
          <li key={v.id}>
            {v.label}{" "}
            <button
              className="button button--secondary"
              type="button"
              disabled={disabled}
              aria-label={`${t.remove}: ${v.label}`}
              onClick={() => onChange(value.filter((x) => x.id !== v.id))}
            >
              {t.remove}
            </button>
          </li>
        ))}
      </ul>
    </div>
  );
}

export function MajorPanel({
  record,
  retain,
}: {
  record: Incident;
  retain: (key: string, dirty: boolean) => void;
}) {
  const locale = useCMDBLocale(),
    t = majorMessages[locale],
    { hasPermission } = useAuth();
  const manage = hasPermission("incident.major.manage"),
    canNominate = hasPermission("incident.major.nominate");
  const [extraPublic, setExtraPublic] = useState<MajorUpdate[]>([]),
    [extraPrivate, setExtraPrivate] = useState<MajorUpdate[]>([]),
    [extraTimeline, setExtraTimeline] = useState<MajorDecision[]>([]),
    [feedCursors, setFeedCursors] = useState<Record<string, string>>({});
  const [view, setView] = useState<MajorView>(),
    [error, setError] = useState<string>(),
    [notice, setNotice] = useState<string>(),
    [busy, setBusy] = useState(false);
  const [dirty, setDirty] = useState<Record<string, boolean>>({});
  const dirtyRef = useRef(dirty);
  dirtyRef.current = dirty;
  const [coord, setCoord] = useState(() => coordinationOf()),
    [reason, setReason] = useState("");
  const [approvedUpdate, setApprovedUpdate] = useState("");
  const [childrenNext, setChildrenNext] = useState("0");
  async function moreChildren() {
    if (busy || childrenNext === "0") return;
    setBusy(true);
    try {
      const page = await api<{ items: typeof children; next_after_id: string }>(
        `/api/v1/incidents/${record.id}/major/children?limit=100&after_id=${childrenNext}`,
      );
      setChildren((old) => unique([...old, ...page.items]));
      setChildrenNext(page.next_after_id);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : t.error);
    } finally {
      setBusy(false);
    }
  }
  const [body, setBody] = useState(""),
    [audience, setAudience] = useState("REQUESTERS");
  const [restoreReason, setRestoreReason] = useState(""),
    [restored, setRestored] = useState(""),
    [detected, setDetected] = useState("");
  const [review, setReview] = useState({ impact: "", causes: "", worked: "" });
  const [task, setTask] = useState({
    description: "",
    owner: null as Choice | null,
    due: "",
  });
  const [reviewTasks, setReviewTasks] = useState<
    { description: string; owner: Choice | null; due: string }[]
  >([]);
  const [children, setChildren] = useState<
      {
        id: string;
        number: string;
        title: string;
        version: string;
        type: string;
      }[]
    >([]),
    [selected, setSelected] = useState<string[]>([]),
    [childResults, setChildResults] = useState<
      { id: string; status: number; message: string }[]
    >([]);
  const mounted = useRef(true),
    retry = useRef<Record<string, { signature: string; key: string }>>({}),
    draftVersions = useRef<Record<string, string>>({});
  const [conflictSection, setConflictSection] = useState<string>();
  const mark = (key: string) => {
    if (!dirtyRef.current[key])
      draftVersions.current[key] =
        key === "review"
          ? (view?.review?.version ?? "0")
          : (view?.case?.version ?? record.version);
    dirtyRef.current = { ...dirtyRef.current, [key]: true };
    setDirty(dirtyRef.current);
  };
  useEffect(() => {
    retain("major", Object.values(dirty).some(Boolean));
  }, [dirty, retain]);
  const load = useCallback(
    async (signal?: AbortSignal) => {
      try {
        const next = await api<MajorView>(
          `/api/v1/incidents/${record.id}/major`,
          { signal },
        );
        if (!mounted.current || signal?.aborted) return;
        setView(next);
        setExtraPublic([]);
        setExtraPrivate([]);
        setExtraTimeline([]);
        setFeedCursors({});
        if (!dirtyRef.current.coord) setCoord(coordinationOf(next.case));
        if (!dirtyRef.current.review)
          setReview({
            impact:
              next.review?.impact_summary ?? next.case?.impact_summary ?? "",
            causes: next.review?.known_causes ?? "",
            worked: next.review?.what_worked ?? "",
          });
        if (!dirtyRef.current.restore) {
          setDetected(localInput(next.case?.detected_at));
          setRestored(localInput(next.case?.restored_at));
        }
        if (manage && next.case) {
          const related = await api<{
            items: {
              id: string;
              number: string;
              title: string;
              version: string;
              state: string;
            }[];
            next_after_id: string;
          }>(`/api/v1/incidents/${record.id}/major/children?limit=100`, {
            signal,
          });
          if (mounted.current && !signal?.aborted) {
            setChildren(related.items.map((v) => ({ ...v, type: "PARENT" })));
            setChildrenNext(related.next_after_id);
          }
        }
      } catch (cause) {
        if (mounted.current && !signal?.aborted) {
          if (
            cause instanceof ApiError &&
            (cause.status === 403 || cause.status === 404)
          ) {
            setView({ restricted: true, public_updates: [] });
            setExtraPublic([]);
            setExtraPrivate([]);
            setExtraTimeline([]);
          }
          setError(cause instanceof Error ? cause.message : t.error);
        }
      }
    },
    [record.id, manage, t.error],
  );
  useEffect(() => {
    mounted.current = true;
    const controller = new AbortController();
    void load(controller.signal);
    return () => {
      mounted.current = false;
      controller.abort();
    };
  }, [load, record.version]);
  async function command(
    section: string,
    path: string,
    value: unknown | (() => unknown),
    method = "POST",
    onSuccess?: () => void,
  ) {
    if (busy) return;
    setBusy(true);
    setError(undefined);
    setNotice(undefined);
    try {
      const resolved = typeof value === "function" ? value() : value;
      const payload =
        resolved &&
        typeof resolved === "object" &&
        !Array.isArray(resolved) &&
        draftVersions.current[section] !== undefined
          ? { ...resolved, version: draftVersions.current[section] }
          : resolved;
      const signature = JSON.stringify({ path, value: payload, method });
      if (retry.current[section]?.signature !== signature)
        retry.current[section] = { signature, key: crypto.randomUUID() };
      const result = await api<{
        results?: { id: string; status: number; message: string }[];
      }>(path, {
        method,
        body: JSON.stringify(payload),
        headers: { "Idempotency-Key": retry.current[section]!.key },
      });
      if (!mounted.current) return;
      delete retry.current[section];
      delete draftVersions.current[section];
      dirtyRef.current = { ...dirtyRef.current, [section]: false };
      setDirty(dirtyRef.current);
      setConflictSection(undefined);
      onSuccess?.();
      if (result?.results) {
        setChildResults(result.results);
        const completed = new Set(
          result.results.filter((v) => v.status < 400).map((v) => v.id),
        );
        setSelected((old) => old.filter((id) => !completed.has(id)));
      }
      setNotice(t.saved);
      await load();
    } catch (cause) {
      if (mounted.current) {
        if (cause instanceof ApiError && cause.status === 409) {
          setConflictSection(section);
          setError(t.conflict);
          await load();
        } else setError(cause instanceof Error ? cause.message : t.error);
      }
    } finally {
      if (mounted.current) setBusy(false);
    }
  }

  async function more(feed: "REQUESTERS" | "OPERATORS" | "TIMELINE") {
    if (busy) return;
    setBusy(true);
    setError(undefined);
    const initial =
      feed === "TIMELINE"
        ? (view?.timeline ?? [])
        : feed === "OPERATORS"
          ? (view?.internal_updates ?? [])
          : (view?.public_updates ?? []);
    const extra =
      feed === "TIMELINE"
        ? extraTimeline
        : feed === "OPERATORS"
          ? extraPrivate
          : extraPublic;
    const before = feedCursors[feed] ?? extra.at(-1)?.id ?? initial.at(-1)?.id;
    try {
      if (feed === "TIMELINE") {
        const page = await api<{
          items: MajorDecision[];
          next_before_id: string;
        }>(
          `/api/v1/incidents/${record.id}/major/timeline?limit=50&before_id=${before}`,
        );
        if (mounted.current) {
          setExtraTimeline((old) => unique([...old, ...page.items]));
          setFeedCursors((old) => ({ ...old, [feed]: page.next_before_id }));
        }
      } else {
        const page = await api<{
          items: MajorUpdate[];
          next_before_id: string;
        }>(
          `/api/v1/incidents/${record.id}/major/updates?limit=50&before_id=${before}&audience=${feed}`,
        );
        if (mounted.current) {
          if (feed === "OPERATORS")
            setExtraPrivate((old) => unique([...old, ...page.items]));
          else setExtraPublic((old) => unique([...old, ...page.items]));
          setFeedCursors((old) => ({ ...old, [feed]: page.next_before_id }));
        }
      }
    } catch (cause) {
      if (mounted.current)
        setError(cause instanceof Error ? cause.message : t.error);
    } finally {
      if (mounted.current) setBusy(false);
    }
  }
  const base = `/api/v1/incidents/${record.id}/major`,
    c = view?.case;
  const coordinate = (
    key: keyof CoordinationDraft,
    value: CoordinationDraft[keyof CoordinationDraft],
  ) => {
    mark("coord");
    setCoord((d) => ({ ...d, [key]: value }));
  };
  function decision(action: string) {
    void command(
      "decision",
      `${base}/decisions/${action}`,
      { version: c?.version ?? record.version, reason },
      "POST",
      () => setReason(""),
    );
  }
  function saveCoord(event: FormEvent) {
    event.preventDefault();
    if (!c) return;
    void command(
      "coord",
      `${base}/coordination`,
      () => ({
        version: c.version,
        commander_user_id: coord.commander?.id ?? null,
        deputy_user_id: coord.deputy?.id ?? null,
        communications_owner_id: coord.owner?.id ?? null,
        bridge_url: coord.bridge,
        impact_summary: coord.impact,
        impact_confidence: coord.confidence,
        workaround: coord.workaround,
        communication_audience: coord.audience,
        update_interval_minutes: coord.interval,
        next_update_at: utc(coord.next),
        detected_at: utc(coord.detected),
        team_group_ids: coord.teams.map((v) => v.id),
        service_ids:
          hasPermission("service_portfolio.read") ||
          hasPermission("service_portfolio.manage")
            ? coord.services.map((v) => v.id)
            : [],
        ci_ids:
          hasPermission("cmdb.read") || hasPermission("cmdb.manage")
            ? coord.cis.map((v) => v.id)
            : [],
      }),
      "PUT",
    );
  }
  const date = (value?: string) =>
    value ? formatDateTime(value, locale) : "—";
  if (!view && !error) return <p role="status">{t.loading}</p>;
  return (
    <section className="module08-major">
      <header className="section-header">
        <h2>{t.coordination}</h2>
        <button
          className="button button--secondary"
          type="button"
          disabled={busy}
          onClick={() => void load()}
        >
          {t.refresh}
        </button>
      </header>
      {error && (
        <p className="notice notice--error" role="alert">
          {error}
        </p>
      )}
      {notice && (
        <p role="status" className="notice">
          {notice}
        </p>
      )}
      {conflictSection && (
        <section className="notice" role="alert">
          <h3>{t.conflict}</h3>
          <dl className="detail-grid">
            <div>
              <dt>{t.version}</dt>
              <dd>
                {conflictSection === "review"
                  ? (view?.review?.version ?? "0")
                  : (view?.case?.version ?? record.version)}
              </dd>
            </div>
            <div>
              <dt>{t.impact}</dt>
              <dd>{view?.case?.impact_summary || "—"}</dd>
            </div>
            <div>
              <dt>{t.workaround}</dt>
              <dd>{view?.case?.workaround || "—"}</dd>
            </div>
            <div>
              <dt>{t.nextUpdate}</dt>
              <dd>
                {view?.case?.next_update_at
                  ? formatDateTime(view.case.next_update_at, locale)
                  : "—"}
              </dd>
            </div>
          </dl>
          <button
            className="button button--secondary"
            type="button"
            disabled={busy}
            onClick={() => {
              draftVersions.current[conflictSection] =
                conflictSection === "review"
                  ? (view?.review?.version ?? "0")
                  : (view?.case?.version ?? record.version);
              delete retry.current[conflictSection];
              setConflictSection(undefined);
              setError(undefined);
            }}
          >
            {t.rebaseDraft}
          </button>
        </section>
      )}
      {c && (
        <dl className="detail-grid">
          <div>
            <dt>{t.status}</dt>
            <dd>{t.stateLabels[c.status] ?? c.status}</dd>
          </div>
          <div>
            <dt>{t.review}</dt>
            <dd>
              {t.stateLabels[c.review_status ?? ""] ?? c.review_status ?? "—"}
            </dd>
          </div>
          <div>
            <dt>{t.lastUpdate}</dt>
            <dd>{date(c.last_update_at)}</dd>
          </div>
          <div>
            <dt>{t.nextUpdate}</dt>
            <dd>{date(c.next_update_at)}</dd>
          </div>
        </dl>
      )}
      {(manage || canNominate) && (
        <fieldset className="form-fields" disabled={busy}>
          <label>
            {t.reason}
            <textarea
              value={reason}
              maxLength={2000}
              onChange={(e) => {
                setReason(e.target.value);
                mark("decision");
              }}
            />
          </label>
          <div className="action-row">
            {canNominate &&
              (!c || ["REJECTED", "DEMOTED"].includes(c.status)) && (
                <button
                  className="button button--secondary"
                  disabled={
                    !reason.trim() ||
                    ["RESOLVED", "CLOSED", "CANCELLED"].includes(record.state)
                  }
                  onClick={() => decision("nominate")}
                >
                  {t.nominate}
                </button>
              )}
            {manage && c?.status === "CANDIDATE" && (
              <>
                <button
                  className="button button--secondary"
                  disabled={!reason.trim()}
                  onClick={() => decision("promote")}
                >
                  {t.promote}
                </button>
                <button
                  className="button button--secondary"
                  disabled={!reason.trim()}
                  onClick={() => decision("reject")}
                >
                  {t.reject}
                </button>
              </>
            )}
            {manage && c?.status === "ACTIVE" && (
              <button
                className="button button--secondary"
                disabled={!reason.trim()}
                onClick={() => decision("demote")}
              >
                {t.demote}
              </button>
            )}
          </div>
        </fieldset>
      )}
      {view?.restricted && <p>{t.publicHint}</p>}
      {c && manage && c.status === "ACTIVE" && (
        <form onSubmit={saveCoord}>
          <h3>{t.coordination}</h3>
          <fieldset disabled={busy} className="form-fields">
            <div className="two-column">
              <RemoteCombobox
                kind="users"
                label={t.commander}
                endpoint="/api/v1/incidents/lookups/users?purpose=coordination"
                value={coord.commander}
                onChange={(v) => coordinate("commander", v)}
              />
              <RemoteCombobox
                kind="users"
                label={t.deputy}
                endpoint="/api/v1/incidents/lookups/users?purpose=coordination"
                value={coord.deputy}
                onChange={(v) => coordinate("deputy", v)}
              />
              <RemoteCombobox
                kind="users"
                label={t.communicationsOwner}
                endpoint="/api/v1/incidents/lookups/users?purpose=coordination"
                value={coord.owner}
                onChange={(v) => coordinate("owner", v)}
              />
              <label>
                {t.bridge}
                <input
                  type="url"
                  value={coord.bridge}
                  maxLength={2048}
                  onChange={(e) => coordinate("bridge", e.target.value)}
                />
              </label>
            </div>
            <label>
              {t.impact}
              <textarea
                maxLength={4000}
                value={coord.impact}
                onChange={(e) => coordinate("impact", e.target.value)}
              />
            </label>
            <label>
              <select
                aria-label={t.confidence}
                value={coord.confidence}
                onChange={(e) => coordinate("confidence", e.target.value)}
              >
                <option value="ESTIMATED">{t.estimated}</option>
                <option value="CONFIRMED">{t.confirmed}</option>
              </select>
            </label>
            <label>
              {t.workaround}
              <textarea
                maxLength={4000}
                value={coord.workaround}
                onChange={(e) => coordinate("workaround", e.target.value)}
              />
            </label>
            <ReferenceList
              label={t.teams}
              value={coord.teams}
              onChange={(v) => coordinate("teams", v)}
              endpoint="/api/v1/incidents/lookups/groups"
              collection="groups"
              cursor="after_id"
            />
            {(hasPermission("service_portfolio.read") ||
              hasPermission("service_portfolio.manage")) && (
              <ReferenceList
                label={t.services}
                value={coord.services}
                onChange={(v) => coordinate("services", v)}
                endpoint="/api/v1/service-portfolio/items?lifecycle=ACTIVE"
              />
            )}
            {(hasPermission("cmdb.read") || hasPermission("cmdb.manage")) && (
              <ReferenceList
                label={t.cis}
                value={coord.cis}
                onChange={(v) => coordinate("cis", v)}
                endpoint="/api/v1/cmdb/cis?archived=false"
              />
            )}
            <label>
              {t.audience}
              <select
                value={coord.audience}
                onChange={(e) => coordinate("audience", e.target.value)}
              >
                <option value="ALL_LINKED">{t.allLinked}</option>
                <option value="REPORTERS_AND_AFFECTED">{t.reporters}</option>
                <option value="WATCHERS">{t.watchers}</option>
              </select>
            </label>
            <div className="two-column">
              <label>
                {t.interval}
                <input
                  type="number"
                  required
                  min={5}
                  max={1440}
                  value={coord.interval}
                  onChange={(e) =>
                    coordinate("interval", Number(e.target.value))
                  }
                />
              </label>
              <OrganizationDateInput
                label={t.nextUpdate}
                value={coord.next}
                onChange={(value) => coordinate("next", value)}
              />
              <OrganizationDateInput
                label={t.detected}
                value={coord.detected}
                onChange={(value) => coordinate("detected", value)}
              />
            </div>
            <button className="button button--primary">{t.save}</button>
          </fieldset>
        </form>
      )}
      {c && !view?.restricted && !manage && (
        <section>
          <dl className="detail-grid">
            <div>
              <dt>{t.commander}</dt>
              <dd>{c.people?.[c.commander_user_id ?? ""] ?? "—"}</dd>
            </div>
            <div>
              <dt>{t.communicationsOwner}</dt>
              <dd>{c.people?.[c.communications_owner_id ?? ""] ?? "—"}</dd>
            </div>
            <div>
              <dt>{t.impact}</dt>
              <dd className="preserve-lines">{c.impact_summary || "—"}</dd>
            </div>
            <div>
              <dt>{t.workaround}</dt>
              <dd className="preserve-lines">{c.workaround || "—"}</dd>
            </div>
            <div>
              <dt>{t.teams}</dt>
              <dd>{c.teams?.map((v) => v.group_name).join(", ") || "—"}</dd>
            </div>
          </dl>
        </section>
      )}
      {c?.bridge_url && (
        <p>
          <a href={c.bridge_url} target="_blank" rel="noreferrer noopener">
            {t.openBridge}
          </a>
        </p>
      )}
      {c && manage && ["ACTIVE", "RESTORED"].includes(c.status) && (
        <form
          aria-label={t.publish}
          onSubmit={(e) => {
            e.preventDefault();
            void command(
              "communication",
              `${base}/updates`,
              { version: c.version, audience, body },
              "POST",
              () => setBody(""),
            );
          }}
        >
          <h3>{t.publish}</h3>
          <fieldset className="form-fields" disabled={busy}>
            <label>
              {t.audience}
              <select
                value={audience}
                onChange={(e) => setAudience(e.target.value)}
              >
                <option value="REQUESTERS">{t.publicUpdates}</option>
                <option value="OPERATORS">{t.operatorUpdates}</option>
              </select>
            </label>
            <label>
              {t.body}
              <textarea
                required
                maxLength={20000}
                value={body}
                onChange={(e) => {
                  setBody(e.target.value);
                  mark("communication");
                }}
              />
            </label>
            <button className="button button--secondary">{t.publish}</button>
          </fieldset>
        </form>
      )}
      {[
        {
          key: "REQUESTERS" as const,
          title: t.publicUpdates,
          items: unique([...(view?.public_updates ?? []), ...extraPublic]),
          initial: view?.public_updates?.length ?? 0,
        },
        {
          key: "OPERATORS" as const,
          title: t.operatorUpdates,
          items: unique([...(view?.internal_updates ?? []), ...extraPrivate]),
          initial: view?.internal_updates?.length ?? 0,
        },
      ]
        .filter((x) => !view?.restricted || x.title === t.publicUpdates)
        .map((feed) => (
          <section key={feed.title}>
            <h3>{feed.title}</h3>
            {!feed.items.length && <p>{t.empty}</p>}
            <ol className="module08-feed">
              {feed.items.map((u) => (
                <li key={u.id}>
                  <time dateTime={u.occurred_at}>{date(u.occurred_at)}</time>
                  {u.actor_name && <span> · {u.actor_name}</span>}
                  <p className="preserve-lines">{u.body}</p>
                  {u.delivery_state && (
                    <small>
                      {t.delivery}:{" "}
                      {t.stateLabels[u.delivery_state] ?? u.delivery_state}
                    </small>
                  )}
                </li>
              ))}
            </ol>
            {(feedCursors[feed.key] !== undefined
              ? feedCursors[feed.key] !== "0"
              : feed.initial >= 100) && (
              <button
                className="button button--secondary"
                disabled={busy}
                onClick={() => void more(feed.key)}
              >
                {t.more}
              </button>
            )}
          </section>
        ))}
      {c && manage && c.status === "ACTIVE" && (
        <form
          onSubmit={(e) => {
            e.preventDefault();
            void command("restore", `${base}/restoration`, () => ({
              version: c.version,
              detected_at: utc(detected),
              restored_at: utc(restored),
              reason: restoreReason,
            }));
          }}
        >
          <h3>{t.restore}</h3>
          <p>{t.restoreHint}</p>
          <fieldset disabled={busy} className="form-fields">
            <div className="two-column">
              <OrganizationDateInput
                required
                label={t.detected}
                value={detected}
                onChange={(value) => {
                  setDetected(value);
                  mark("restore");
                }}
              />
              <OrganizationDateInput
                required
                label={t.restored}
                value={restored}
                onChange={(value) => {
                  setRestored(value);
                  mark("restore");
                }}
              />
            </div>
            <label>
              {t.reason}
              <textarea
                required
                maxLength={2000}
                value={restoreReason}
                onChange={(e) => {
                  setRestoreReason(e.target.value);
                  mark("restore");
                }}
              />
            </label>
            <button
              className="button button--secondary"
              disabled={record.state !== "RESOLVED" || !restoreReason.trim()}
            >
              {t.restore}
            </button>
          </fieldset>
        </form>
      )}
      {c && !view?.restricted && (
        <>
          <h3>{t.technicalActions}</h3>
          <MajorActionList
            items={view?.actions ?? []}
            busy={busy}
            change={(action, status) =>
              void command(
                "actions",
                `/api/v1/incidents/major/actions/${action.id}`,
                { version: action.version, status },
                "PUT",
              )
            }
          />
        </>
      )}
      {c && manage && ["ACTIVE", "RESTORED"].includes(c.status) && (
        <form
          onSubmit={(e) => {
            e.preventDefault();
            void command(
              "task",
              `${base}/actions`,
              () => ({
                version: c.version,
                description: task.description,
                owner_user_id: task.owner?.id ?? null,
                due_at: utc(task.due),
              }),
              "POST",
              () => setTask({ description: "", owner: null, due: "" }),
            );
          }}
        >
          <h3>{t.createAction}</h3>
          <fieldset disabled={busy} className="form-fields">
            <label>
              {t.actionDescription}
              <textarea
                required
                maxLength={4000}
                value={task.description}
                onChange={(e) => {
                  setTask({ ...task, description: e.target.value });
                  mark("task");
                }}
              />
            </label>
            <RemoteCombobox
              kind="users"
              label={t.owner}
              value={task.owner}
              onChange={(owner) => {
                setTask({ ...task, owner });
                mark("task");
              }}
              endpoint="/api/v1/incidents/lookups/users?purpose=coordination"
            />
            <OrganizationDateInput
              required
              label={t.due}
              value={task.due}
              onChange={(value) => {
                setTask({ ...task, due: value });
                mark("task");
              }}
            />
            <button className="button button--secondary">
              {t.createAction}
            </button>
          </fieldset>
        </form>
      )}
      {c && manage && c.status === "RESTORED" && (
        <form
          onSubmit={(e) => {
            e.preventDefault();
            const complete =
              (e.nativeEvent as SubmitEvent).submitter?.getAttribute(
                "value",
              ) === "complete";
            void command(
              "review",
              `${base}/review`,
              () => ({
                version: view?.review?.version ?? "0",
                detected_at: c.detected_at,
                restored_at: c.restored_at,
                impact_summary: review.impact,
                known_causes: review.causes,
                what_worked: review.worked,
                complete,
                actions: reviewTasks.map((v) => ({
                  description: v.description,
                  owner_user_id: v.owner?.id ?? null,
                  due_at: utc(v.due),
                  status: "OPEN",
                })),
              }),
              "PUT",
              () => setReviewTasks([]),
            );
          }}
        >
          <h3>{t.review}</h3>
          <p>{t.reviewSeparate}</p>
          {view?.review?.completed_at && (
            <p role="status">
              {t.reviewComplete}: {date(view.review.completed_at)}
            </p>
          )}
          <fieldset
            disabled={busy || Boolean(view?.review?.completed_at)}
            className="form-fields"
          >
            {(
              [
                ["impact", t.impact],
                ["causes", t.causes],
                ["worked", t.worked],
              ] as const
            ).map(([key, label]) => (
              <label key={key}>
                {label}
                <textarea
                  maxLength={8000}
                  value={review[key]}
                  onChange={(e) => {
                    setReview({ ...review, [key]: e.target.value });
                    mark("review");
                  }}
                />
              </label>
            ))}
            {reviewTasks.map((action, index) => (
              <fieldset key={index}>
                <legend>
                  {t.reviewAction} {index + 1}
                </legend>
                <label>
                  {t.actionDescription}
                  <textarea
                    required
                    value={action.description}
                    onChange={(e) => {
                      mark("review");
                      setReviewTasks((v) =>
                        v.map((x, i) =>
                          i === index
                            ? { ...x, description: e.target.value }
                            : x,
                        ),
                      );
                    }}
                  />
                </label>
                <RemoteCombobox
                  kind="users"
                  label={t.owner}
                  endpoint="/api/v1/incidents/lookups/users?purpose=coordination"
                  value={action.owner}
                  onChange={(owner) => {
                    mark("review");
                    setReviewTasks((v) =>
                      v.map((x, i) => (i === index ? { ...x, owner } : x)),
                    );
                  }}
                />
                <OrganizationDateInput
                  required
                  label={t.due}
                  value={action.due}
                  onChange={(value) => {
                    mark("review");
                    setReviewTasks((v) =>
                      v.map((x, i) => (i === index ? { ...x, due: value } : x)),
                    );
                  }}
                />
                <button
                  className="button button--secondary"
                  type="button"
                  onClick={() =>
                    setReviewTasks((v) => v.filter((_, i) => i !== index))
                  }
                >
                  {t.remove}
                </button>
              </fieldset>
            ))}
            <button
              className="button button--secondary"
              type="button"
              onClick={() => {
                mark("review");
                setReviewTasks((v) => [
                  ...v,
                  { description: "", owner: null, due: "" },
                ]);
              }}
            >
              {t.addReviewAction}
            </button>
            <div className="action-row">
              <button className="button button--secondary" value="draft">
                {t.save}
              </button>
              <button className="button button--secondary" value="complete">
                {t.completeReview}
              </button>
            </div>
          </fieldset>
        </form>
      )}
      {c && manage && c.status === "ACTIVE" && (
        <section>
          <h3>{t.children}</h3>
          <p>{t.childrenHint}</p>
          <label>
            {t.publicUpdates}
            <select
              value={approvedUpdate}
              onChange={(e) => setApprovedUpdate(e.target.value)}
            >
              <option value="">—</option>
              {view?.public_updates.map((v) => (
                <option key={v.id} value={v.id}>
                  {date(v.occurred_at)} · {v.body.slice(0, 100)}
                </option>
              ))}
            </select>
          </label>
          {children.map((child) => (
            <label key={child.id}>
              <input
                type="checkbox"
                checked={selected.includes(child.id)}
                disabled={
                  busy ||
                  (!selected.includes(child.id) && selected.length >= 50)
                }
                onChange={(e) =>
                  setSelected((v) =>
                    e.target.checked
                      ? [...v, child.id]
                      : v.filter((x) => x !== child.id),
                  )
                }
              />
              <Link to={`/incidents/${child.id}`}>
                {child.number} · {child.title}
              </Link>
            </label>
          ))}
          {childrenNext !== "0" && (
            <button
              className="button button--secondary"
              disabled={busy}
              onClick={() => void moreChildren()}
            >
              {t.more}
            </button>
          )}
          <div className="action-row">
            {["message", "solution"].map((action) => (
              <button
                className="button button--secondary"
                key={action}
                disabled={
                  busy ||
                  !selected.length ||
                  (action === "message"
                    ? !approvedUpdate
                    : !body.trim() && !approvedUpdate)
                }
                onClick={() =>
                  void command("children", `${base}/children`, {
                    version: c.version,
                    action,
                    update_id:
                      action === "message" ? approvedUpdate : undefined,
                    message:
                      body ||
                      view?.public_updates.find((v) => v.id === approvedUpdate)
                        ?.body ||
                      "",
                    items: children
                      .filter((x) => selected.includes(x.id))
                      .map((x) => ({ id: x.id, version: x.version })),
                  })
                }
              >
                {action === "message" ? t.messageChildren : t.solutionChildren}
              </button>
            ))}
          </div>
          {childResults.length > 0 && (
            <ul aria-live="polite">
              {childResults.map((result) => (
                <li key={result.id}>
                  {children.find((x) => x.id === result.id)?.number ??
                    result.id}
                  : {result.status} · {result.message}
                </li>
              ))}
            </ul>
          )}
        </section>
      )}
      {view?.timeline && (
        <section>
          <h3>{t.timeline}</h3>
          <ol className="module08-feed">
            {unique([...view.timeline, ...extraTimeline]).map((v) => (
              <li key={v.id}>
                <time>{date(v.occurred_at)}</time> · {v.actor_name} ·{" "}
                {t.stateLabels[v.type] ?? v.type}
                <p>{v.reason}</p>
              </li>
            ))}
          </ol>
          {(feedCursors.TIMELINE !== undefined
            ? feedCursors.TIMELINE !== "0"
            : view.timeline.length >= 100) && (
            <button
              className="button button--secondary"
              disabled={busy}
              onClick={() => void more("TIMELINE")}
            >
              {t.more}
            </button>
          )}
        </section>
      )}
    </section>
  );
}

export function MajorActionList({
  items,
  busy,
  change,
}: {
  items: MajorAction[];
  busy: boolean;
  change: (item: MajorAction, state: string) => void;
}) {
  const locale = useCMDBLocale(),
    t = majorMessages[locale],
    { session, hasPermission } = useAuth();
  return (
    <>
      {!items.length && <p>{t.empty}</p>}
      <ul className="module08-actions">
        {items.map((item) => (
          <li key={item.id}>
            <Link to={`/workspace/major?view=actions&action_id=${item.id}`}>
              {item.number}
            </Link>
            <p className="preserve-lines">{item.description}</p>
            <p>
              {t.owner}: {item.owner_name || "—"} · {t.due}:{" "}
              {formatDateTime(item.due_at, locale)} ·{" "}
              {t.stateLabels[item.status] ?? item.status}
            </p>
            {["OPEN", "IN_PROGRESS"].includes(item.status) &&
              (hasPermission("incident.major.manage") ||
                item.owner_user_id === session?.user.id) && (
                <div className="action-row">
                  {item.status === "OPEN" && (
                    <button
                      className="button button--secondary"
                      disabled={busy}
                      onClick={() => change(item, "IN_PROGRESS")}
                    >
                      {t.start}
                    </button>
                  )}
                  <button
                    className="button button--secondary"
                    disabled={busy}
                    onClick={() => change(item, "DONE")}
                  >
                    {t.done}
                  </button>
                  <button
                    className="button button--secondary"
                    disabled={busy}
                    onClick={() => change(item, "CANCELLED")}
                  >
                    {t.cancel}
                  </button>
                </div>
              )}
          </li>
        ))}
      </ul>
    </>
  );
}

function unique<T extends { id: string }>(items: T[]): T[] {
  const seen = new Set<string>();
  return items.filter((item) => {
    if (seen.has(item.id)) return false;
    seen.add(item.id);
    return true;
  });
}
