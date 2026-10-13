import { useEffect, useRef, useState } from "react";
import {
  Link,
  useNavigate,
  useParams,
  useSearchParams,
} from "react-router-dom";
import { useAuth } from "../auth/AuthContext";
import { useIncidentQuery } from "../incidents/hooks";
import { UnsavedGuard } from "../incidents/UnsavedGuard";
import { PageState } from "../components/PageState";
import { useRemotePage } from "../components/useRemotePage";
import { InventoryChoice, More, PageFeedback } from "../cmdb/components";
import { type Choice } from "../components/RemoteCombobox";
import { useCreation } from "../requests/useCreation";
import { useEditor } from "../requests/useEditor";
import { DateTimeField } from "../changes/DateTimeField";
import { CIReference } from "../changes/CISet";
import { ConfigurationFeedback } from "../changes/ConfigurationFeedback";
import { localCandidates } from "../changes/time";
import { labels, type CalendarRule, type Change } from "../changes/contracts";
type Calendar = { changes: Change[]; rules: CalendarRule[] };
const dateLabel = (value: string, zone: string) =>
  new Date(value).toLocaleString("pl-PL", { timeZone: zone });
function shift(date: string, days: number) {
  return new Date(new Date(date + "T12:00:00Z").getTime() + days * 86400000)
    .toISOString()
    .slice(0, 10);
}
function midnight(date: string, zone: string) {
  for (let minute = 0; minute < 180; minute++) {
    const text = `${date}T${String(Math.floor(minute / 60)).padStart(2, "0")}:${String(minute % 60).padStart(2, "0")}`;
    const c = localCandidates(text, zone);
    if (c.length) return c[0]!;
  }
  throw new Error("Dzień nie ma poprawnego początku w tej strefie.");
}
export function ChangeCalendarPage() {
  const { ruleID } = useParams(),
    { hasPermission } = useAuth();
  const p = useIncidentQuery<CalendarRule>(
    ruleID && ruleID !== "new"
      ? `/api/v1/changes/calendar-rules/${ruleID}`
      : null,
  );
  if (ruleID) {
    if (!hasPermission("change.calendar"))
      return <PageState title="Brak uprawnień do reguł kalendarza" />;
    if (ruleID === "new")
      return (
        <RuleEditor
          initial={{
            id: "0",
            version: "0",
            title: "",
            kind: "WINDOW",
            ci_id: null,
            starts_at: "",
            ends_at: "",
            time_zone: "Europe/Warsaw",
            active: true,
          }}
        />
      );
    if (!p.data)
      return (
        <>
          <PageFeedback loading={p.loading} error={p.error} empty={false} />
          <button onClick={p.reload}>Ponów odczyt reguły</button>
        </>
      );
    return <RuleEditor key={ruleID} initial={p.data} />;
  }
  if (!["change.read", "change.read_own"].some(hasPermission))
    return <PageState title="Brak uprawnień do kalendarza zmian" />;
  return <CalendarView />;
}
function CalendarView() {
  const [params, setParams] = useSearchParams(),
    { hasPermission } = useAuth();
  const anchor =
      params.get("date") ??
      new Date().toLocaleDateString("en-CA", { timeZone: "Europe/Warsaw" }),
    zone = params.get("zone") ?? "Europe/Warsaw",
    mode = params.get("mode") === "week" ? "week" : "month";
  const [date, setDate] = useState(anchor),
    [timeZone, setTimeZone] = useState(zone);
  useEffect(() => {
    setDate(anchor);
    setTimeZone(zone);
  }, [anchor, zone]);
  let error = "",
    from = "",
    to = "",
    days: string[] = [];
  try {
    if (!/^\d{4}-\d{2}-\d{2}$/.test(anchor) || shift(anchor, 0) !== anchor)
      throw new Error("Wybierz poprawną datę.");
    let start = anchor,
      end = shift(start, 7);
    if (mode === "month") {
      start = anchor.slice(0, 8) + "01";
      const d = new Date(start + "T12:00:00Z");
      d.setUTCMonth(d.getUTCMonth() + 1);
      end = d.toISOString().slice(0, 10);
    }
    from = midnight(start, zone);
    to = midnight(end, zone);
    for (let day = start; day < end; day = shift(day, 1)) days.push(day);
  } catch (e) {
    error = e instanceof Error ? e.message : "Nieprawidłowy zakres.";
  }
  const p = useIncidentQuery<Calendar>(
    !error
      ? `/api/v1/changes/calendar?from=${encodeURIComponent(from)}&to=${encodeURIComponent(to)}`
      : null,
  );
  const rules = useRemotePage<CalendarRule>(
    "/api/v1/changes/calendar-rules?limit=25",
    "items",
    "before_id",
    hasPermission("change.calendar"),
  );
  function changeDate(days: number) {
    let date = shift(anchor, days);
    if (mode === "month") {
      const d = new Date(anchor.slice(0, 8) + "01T12:00:00Z");
      d.setUTCMonth(d.getUTCMonth() + (days < 0 ? -1 : 1));
      date = d.toISOString().slice(0, 10);
    }
    setParams({ date, zone, mode });
  }
  return (
    <>
      <header className="page-heading">
        <div>
          <p className="eyebrow">Change Management</p>
          <h1>Kalendarz zmian</h1>
        </div>
        <Link className="button" to="/changes">
          Lista zmian
        </Link>
        {hasPermission("change.calendar") && (
          <Link
            className="button button--primary"
            to="/change-calendar/rules/new"
          >
            Nowa reguła kalendarza
          </Link>
        )}
      </header>
      <form
        className="filter-grid"
        onSubmit={(e) => {
          e.preventDefault();
          setParams({ date, zone: timeZone, mode });
        }}
      >
        <label>
          Dzień zakresu
          <input
            type="date"
            required
            value={date}
            onChange={(e) => setDate(e.target.value)}
          />
        </label>
        <label>
          Strefa IANA
          <input
            required
            maxLength={64}
            value={timeZone}
            onChange={(e) => setTimeZone(e.target.value)}
          />
        </label>
        <label>
          Widok
          <select
            value={mode}
            onChange={(e) =>
              setParams({ date: anchor, zone, mode: e.target.value })
            }
          >
            <option value="month">Miesiąc</option>
            <option value="week">7 dni</option>
          </select>
        </label>
        <button>Pokaż zakres</button>
        <button type="button" onClick={p.reload}>
          Odśwież kalendarz
        </button>
      </form>
      <div className="action-row">
        <button
          disabled={!!error}
          onClick={() => changeDate(mode === "week" ? -7 : -28)}
        >
          Poprzedni zakres
        </button>
        <button
          disabled={!!error}
          onClick={() => changeDate(mode === "week" ? 7 : 32)}
        >
          Następny zakres
        </button>
      </div>
      <p>
        Strefa: {zone}. Przedziały obejmują początek i wyłączają koniec.
        Pokazujemy dostępne zmiany od autoryzacji do zamknięcia oraz aktywne
        reguły. Zapis zaplanowanej zmiany ponownie i atomowo sprawdza kolizje.
      </p>
      {error ? (
        <p role="alert">{error}</p>
      ) : (
        <>
          <PageFeedback
            loading={p.loading}
            error={p.error}
            empty={!!p.data && !p.data.changes.length && !p.data.rules.length}
          />
          {p.data && (
            <div className="change-calendar" aria-label="Dni kalendarza zmian">
              {days.map((day) => {
                const start = midnight(day, zone),
                  end = midnight(shift(day, 1), zone),
                  items = p.data!.changes.filter(
                    (r) =>
                      Date.parse(r.planned_start!) < Date.parse(end) &&
                      Date.parse(r.planned_end!) > Date.parse(start),
                  ),
                  r = p.data!.rules.filter(
                    (r) =>
                      Date.parse(r.starts_at) < Date.parse(end) &&
                      Date.parse(r.ends_at) > Date.parse(start),
                  );
                return (
                  <section className="calendar-day" key={day}>
                    <h2>
                      {new Date(day + "T12:00:00Z").toLocaleDateString(
                        "pl-PL",
                        {
                          weekday: "short",
                          day: "numeric",
                          month: "short",
                          timeZone: "UTC",
                        },
                      )}
                    </h2>
                    <ul>
                      {items.map((c) => (
                        <li className="calendar-change" key={c.id}>
                          <Link to={`/changes/${c.id}`}>
                            {c.number} · {c.title}
                          </Link>
                          <small>
                            {labels[c.state]} · {labels[c.risk]}
                          </small>
                          <small>
                            {dateLabel(c.planned_start!, zone)} →{" "}
                            {dateLabel(c.planned_end!, zone)}
                          </small>
                        </li>
                      ))}
                      {r.map((v) => (
                        <li
                          className={`calendar-rule ${v.kind === "BLACKOUT" ? "blackout" : "window"}`}
                          key={v.id}
                        >
                          {hasPermission("change.calendar") ? (
                            <Link to={`/change-calendar/rules/${v.id}`}>
                              {v.title}
                            </Link>
                          ) : (
                            v.title
                          )}
                          <small>
                            {v.kind === "WINDOW"
                              ? "Okno serwisowe"
                              : "Zakaz zmian"}{" "}
                            · {v.ci_id ? "Wybrany CI" : "Cała organizacja"}
                          </small>
                          <small>
                            {dateLabel(v.starts_at, zone)} →{" "}
                            {dateLabel(v.ends_at, zone)}
                          </small>
                        </li>
                      ))}
                    </ul>
                    {!items.length && !r.length && <small>Brak wpisów</small>}
                  </section>
                );
              })}
            </div>
          )}
        </>
      )}
      {hasPermission("change.calendar") && (
        <section className="panel">
          <h2>Reguły kalendarza</h2>
          <p>
            Reguły obejmują całą organizację lub wskazany CI. Nieaktywne reguły
            zachowują historię. Okno serwisowe wymaga zawarcia całego terminu
            zmiany.
          </p>
          <PageFeedback
            loading={rules.loading}
            error={rules.error}
            empty={!rules.items.length}
          />
          <ul>
            {rules.items.map((r) => (
              <li key={r.id}>
                <Link to={`/change-calendar/rules/${r.id}`}>{r.title}</Link> ·{" "}
                {r.kind === "WINDOW" ? "Okno serwisowe" : "Zakaz zmian"} ·{" "}
                {r.active ? "Aktywna" : "Nieaktywna"} ·{" "}
                {dateLabel(r.starts_at, r.time_zone)} →{" "}
                {dateLabel(r.ends_at, r.time_zone)} · {r.time_zone}
                {r.ci_id && (
                  <>
                    {" "}
                    · <CIReference id={r.ci_id} />
                  </>
                )}
              </li>
            ))}
          </ul>
          <More loading={rules.loading} next={rules.next} load={rules.more} />
        </section>
      )}
    </>
  );
}
function RuleEditor({ initial }: { initial: CalendarRule }) {
  const path = `/api/v1/changes/calendar-rules/${initial.id}`,
    ed = useEditor(initial, path),
    creation = useCreation<CalendarRule>(),
    navigate = useNavigate();
  const [draft, setDraft] = useState(initial),
    [saved, setSaved] = useState(""),
    [choice, setChoice] = useState<Choice | null>(
      initial.ci_id
        ? { id: initial.ci_id, label: `CI #${initial.ci_id}` }
        : null,
    ),
    keep = useRef(false);
  const existing = initial.id !== "0",
    dirty = JSON.stringify(draft) !== JSON.stringify(ed.record),
    busy = ed.busy || creation.busy,
    uncertain = ed.uncertain || creation.uncertain,
    blocked = busy || uncertain || !!ed.conflict;
  useEffect(() => {
    if (keep.current) {
      keep.current = false;
      return;
    }
    setDraft(ed.record);
    setChoice(
      ed.record.ci_id
        ? { id: ed.record.ci_id, label: `CI #${ed.record.ci_id}` }
        : null,
    );
  }, [ed.record]);
  useEffect(() => {
    if (saved) navigate(saved, { replace: true });
  }, [saved, navigate]);
  return (
    <>
      <UnsavedGuard dirty={!saved && (dirty || uncertain)} includeSearch />
      <header className="page-heading">
        <h1>{existing ? "Reguła kalendarza" : "Nowa reguła kalendarza"}</h1>
        <Link className="button" to="/change-calendar">
          Wróć do kalendarza
        </Link>
      </header>
      <ConfigurationFeedback
        editor={ed}
        proposal={draft}
        current={ed.conflict}
        resolve={(replace) => {
          const current = ed.conflict!;
          if (!replace) {
            keep.current = true;
            setDraft({ ...draft, version: current.version });
          }
          ed.resolve();
        }}
      />
      <form
        onSubmit={async (e) => {
          e.preventDefault();
          if (existing)
            await ed.save(path, draft, "PUT", {
              "Idempotency-Key": crypto.randomUUID(),
            });
          else {
            const r = await creation.create(
              "/api/v1/changes/calendar-rules",
              draft,
            );
            if (r) setSaved(`/change-calendar/rules/${r.id}`);
          }
        }}
      >
        <fieldset disabled={blocked} className="form-grid">
          <label>
            Nazwa reguły
            <input
              required
              maxLength={160}
              value={draft.title}
              onChange={(e) => setDraft({ ...draft, title: e.target.value })}
            />
          </label>
          <label>
            Rodzaj
            <select
              value={draft.kind}
              onChange={(e) => setDraft({ ...draft, kind: e.target.value })}
            >
              <option value="WINDOW">Okno serwisowe</option>
              <option value="BLACKOUT">Zakaz zmian</option>
            </select>
          </label>
          <label>
            Strefa IANA
            <input
              required
              maxLength={64}
              value={draft.time_zone}
              onChange={(e) =>
                setDraft({ ...draft, time_zone: e.target.value })
              }
            />
          </label>
          <DateTimeField
            label="Początek reguły"
            zone={draft.time_zone}
            value={draft.starts_at || null}
            onChange={(v) => setDraft({ ...draft, starts_at: v ?? "" })}
          />
          <DateTimeField
            label="Koniec reguły"
            zone={draft.time_zone}
            value={draft.ends_at || null}
            onChange={(v) => setDraft({ ...draft, ends_at: v ?? "" })}
          />
          <InventoryChoice
            label="CI reguły (puste: cała organizacja)"
            endpoint="/api/v1/cmdb/cis?archived=false"
            value={choice}
            disabled={blocked}
            onChange={(v) => {
              setChoice(v);
              setDraft({ ...draft, ci_id: v?.id ?? null });
            }}
          />
          {draft.ci_id && <CIReference id={draft.ci_id} />}
          <label className="checkbox-label">
            <input
              type="checkbox"
              checked={draft.active}
              onChange={(e) => setDraft({ ...draft, active: e.target.checked })}
            />
            Aktywna reguła
          </label>
        </fieldset>
        {creation.error && <p role="alert">{creation.error}</p>}
        {creation.uncertain && (
          <p role="status">
            Ponowienie zachowa treść i klucz ostatniej reguły.
          </p>
        )}
        <button
          className="button button--primary"
          disabled={
            busy ||
            !!ed.conflict ||
            (!uncertain &&
              (!draft.title.trim() ||
                !draft.starts_at ||
                !draft.ends_at ||
                (existing && !dirty)))
          }
        >
          {creation.uncertain ? "Potwierdź zapis reguły" : "Zapisz regułę"}
        </button>
        {existing && (
          <button
            type="button"
            disabled={blocked || dirty}
            onClick={() => void ed.save(path, undefined, "GET")}
          >
            Odśwież regułę
          </button>
        )}
      </form>
    </>
  );
}
