import { useEffect, useRef, useState, type FormEvent } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { api, ApiError, jsonBody } from "../api/client";
import { PageState } from "../components/PageState";
import { ConflictNotice } from "../components/ConflictNotice";
import { useIncidentQuery } from "../incidents/hooks";
import { dateLabel } from "../incidents/contracts";
import { UnsavedGuard } from "../incidents/UnsavedGuard";
import {
  parseWindows,
  windowsText,
  type Calendar,
  type Policy,
  type Definition,
  type DefinitionPage,
} from "../sla/contracts";

export function SlaAdminPage() {
  const [params] = useSearchParams();
  const kind = params.get("kind") === "calendar" ? "calendar" : "policy";
  const id = params.get("id");
  return (
    <section className="page sla-page">
      <div className="page-heading">
        <div>
          <p className="eyebrow">Administracja</p>
          <h1>SLA i kalendarze</h1>
          <p>
            Wersje opublikowane są niezmienne. Zapis tworzy nową wersję dla
            nowych incydentów.
          </p>
        </div>
      </div>
      <nav className="tabs" aria-label="Administracja SLA">
        <Link
          className={kind === "policy" ? "is-active" : ""}
          to="?kind=policy"
        >
          Polityki SLA
        </Link>
        <Link
          className={kind === "calendar" ? "is-active" : ""}
          to="?kind=calendar"
        >
          Kalendarze
        </Link>
      </nav>
      {id ? (
        <EditorLoader key={`${kind}:${id}`} kind={kind} id={id} />
      ) : (
        <DefinitionList kind={kind} />
      )}
    </section>
  );
}
function DefinitionList({ kind }: { kind: "policy" | "calendar" }) {
  const [params, setParams] = useSearchParams();
  const list = useIncidentQuery<DefinitionPage>(
    `/api/v1/sla/definitions/${kind}?before_id=${params.get("before_id") ?? ""}`,
  );
  return (
    <div className="panel">
      <div className="page-heading">
        <h2>{kind === "policy" ? "Polityki SLA" : "Kalendarze"}</h2>
        <Link className="button button--primary" to={`?kind=${kind}&id=new`}>
          {kind === "policy" ? "Nowa polityka SLA" : "Nowy kalendarz"}
        </Link>
      </div>
      {list.loading ? (
        <PageState title="Ładowanie definicji…" />
      ) : list.error ? (
        <>
          <PageState
            title="Błąd odczytu definicji"
            tone="error"
            detail={list.error}
          />
          <button onClick={list.reload}>Spróbuj ponownie</button>
        </>
      ) : !list.data?.items.length ? (
        <PageState
          title="Brak definicji"
          detail="Utwórz kalendarz, a następnie politykę. SLA nie jest nakładane wstecz na starsze incydenty."
        />
      ) : (
        <div className="table-scroll" tabIndex={0}>
          <table>
            <thead>
              <tr>
                <th scope="col">Nazwa</th>
                <th scope="col">Wersja</th>
                <th scope="col">Obowiązuje od</th>
                <th scope="col">Dopasowanie / strefa</th>
              </tr>
            </thead>
            <tbody>
              {list.data.items.map((d) => (
                <tr key={d.id}>
                  <td>
                    <Link to={`?kind=${kind}&id=${d.id}`}>{d.config.name}</Link>
                  </td>
                  <td>{d.version}</td>
                  <td>{dateLabel(d.effective_at)}</td>
                  <td>
                    {d.kind === "calendar"
                      ? (d.config as Calendar).timezone
                      : `Kolejność ${(d.config as Policy).order}, ${(d.config as Policy).enabled ? "aktywna" : "wyłączona"}, priorytet ${(d.config as Policy).priority || "dowolny"}, kategoria ${(d.config as Policy).category || "dowolna"}`}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      <div className="action-row">
        {params.has("before_id") && (
          <button onClick={() => setParams({ kind })}>Pierwsza strona</button>
        )}
        {list.data?.next_before_id && (
          <button
            onClick={() =>
              setParams({ kind, before_id: list.data!.next_before_id! })
            }
          >
            Następna strona
          </button>
        )}
      </div>
      <details>
        <summary>Reguły wyboru i zmian</summary>
        <p>
          Organizacja pochodzi z sesji. Wygrywa najmniejsza kolejność, następnie
          największa liczba warunków (priorytet/kategoria), następnie
          najmniejszy identyfikator. Wybierana jest wersja obowiązująca przy
          utworzeniu incydentu.
        </p>
        <p>{list.data?.rules}</p>
        <p>
          Maksymalnie 100 definicji każdego typu. Wyłączenie polityki zatrzymuje
          wybór dla nowych incydentów; istniejące cykle zachowują jej wersję.
        </p>
      </details>
    </div>
  );
}
function EditorLoader({
  kind,
  id,
}: {
  kind: "policy" | "calendar";
  id: string;
}) {
  const current = useIncidentQuery<Definition>(
    id === "new" ? null : `/api/v1/sla/definitions/${kind}/${id}`,
    id,
  );
  if (id === "new") return <DefinitionEditor kind={kind} />;
  if (current.loading) return <PageState title="Ładowanie definicji…" />;
  if (!current.data)
    return (
      <>
        <PageState
          title="Nie udało się odczytać definicji"
          detail={current.error}
          tone="error"
        />
        <button onClick={current.reload}>Spróbuj ponownie</button>
      </>
    );
  return <DefinitionEditor kind={kind} initial={current.data} />;
}
const initialCalendar: Calendar = {
  name: "",
  timezone: "Europe/Warsaw",
  week: [1, 2, 3, 4, 5].map((weekday) => ({
    weekday,
    windows: [{ start: "09:00", end: "17:00" }],
  })),
  holidays: [],
  exceptions: [],
};
const initialPolicy: Policy = {
  name: "",
  enabled: true,
  order: 100,
  priority: 0,
  category: "",
  calendar_id: "",
  calendar_version: "",
  response_seconds: 3600,
  resolution_seconds: 28800,
  warning_percent: 80,
  pause_reasons: [],
};
function DefinitionEditor({
  kind,
  initial,
}: {
  kind: "policy" | "calendar";
  initial?: Definition;
}) {
  const [current, setCurrent] = useState(initial);
  const [draft, setDraft] = useState<Calendar | Policy>(
    initial?.config ?? (kind === "calendar" ? initialCalendar : initialPolicy),
  );
  const [conflict, setConflict] = useState<Definition>();
  const [conflictBlocked, setConflictBlocked] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  const [notice, setNotice] = useState<string>();
  const [key] = useState(() => crypto.randomUUID());
  const pending = useRef<
    { version: string; config: Calendar | Policy } | undefined
  >(undefined);
  const controller = useRef<AbortController | undefined>(undefined);
  const archiveController = useRef<AbortController | undefined>(undefined);
  const mounted = useRef(true);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
      controller.current?.abort();
      archiveController.current?.abort();
    };
  }, []);
  const [calendarDirty, setCalendarDirty] = useState(false);
  const [calendarError, setCalendarError] = useState<string>();
  const [editorRevision, setEditorRevision] = useState(0);
  const calendars = useIncidentQuery<DefinitionPage>(
    "/api/v1/sla/definitions/calendar?limit=100",
  );
  const [archiveVersion, setArchiveVersion] = useState("");
  const [archive, setArchive] = useState<Definition>();
  const dirty =
    JSON.stringify(draft) !==
    JSON.stringify(
      current?.config ??
        (kind === "calendar" ? initialCalendar : initialPolicy),
    );
  const update = (patch: Partial<Calendar & Policy>) => {
    setDraft((d) => ({ ...d, ...patch }));
    setNotice(undefined);
  };
  async function save(event?: FormEvent, reconcile = false) {
    event?.preventDefault();
    setError(undefined);
    setNotice(undefined);
    if (calendarError && !reconcile) {
      setError(calendarError);
      return;
    }
    const normalized =
      kind === "policy"
        ? {
            ...draft,
            pause_reasons: (draft as Policy).pause_reasons
              .map((s) => s.trim())
              .filter(Boolean),
          }
        : draft;
    const body = reconcile
      ? pending.current
      : { version: current?.version ?? "0", config: normalized };
    if (!body) return;
    if (
      !current &&
      pending.current &&
      !reconcile &&
      JSON.stringify(body.config) !== JSON.stringify(pending.current.config)
    ) {
      setError(
        "Wynik poprzedniej próby jest niepewny. Najpierw sprawdź jej wynik przyciskiem poniżej; lokalna propozycja pozostanie zachowana.",
      );
      return;
    }
    pending.current = body;
    setBusy(true);
    controller.current?.abort();
    const request = new AbortController();
    controller.current = request;
    try {
      const saved = await api<Definition>(
        `/api/v1/sla/definitions/${kind}${current ? `/${current.id}` : ""}`,
        {
          method: current ? "PUT" : "POST",
          body: jsonBody(body),
          headers: { "Idempotency-Key": key },
          signal: request.signal,
        },
      );
      if (!mounted.current || request.signal.aborted) return;
      setCurrent(saved);
      setCalendarDirty(false);
      setCalendarError(undefined);
      if (!reconcile || JSON.stringify(draft) === JSON.stringify(body.config))
        setDraft(saved.config);
      setNotice(
        "Zapisano wersję definicji. Istniejące SLA zachowują poprzednią politykę i kalendarz.",
      );
      pending.current = undefined;
      setConflict(undefined);
      setConflictBlocked(false);
    } catch (cause) {
      if (!mounted.current || request.signal.aborted) return;
      setError(
        cause instanceof Error
          ? cause.message
          : "Nie udało się zapisać definicji.",
      );
      if (cause instanceof ApiError && cause.status === 409 && current) {
        setConflictBlocked(true);
        try {
          const latest = await api<Definition>(
            `/api/v1/sla/definitions/${kind}/${current.id}`,
            { signal: request.signal },
          );
          if (mounted.current && !request.signal.aborted) setConflict(latest);
        } catch {
          if (mounted.current)
            setError(
              "Konflikt wersji. Nie udało się odczytać aktualnego stanu; propozycja pozostaje w formularzu.",
            );
        }
      } else if (
        cause instanceof ApiError &&
        cause.status >= 400 &&
        cause.status < 500
      )
        pending.current = undefined;
    } finally {
      if (mounted.current && !request.signal.aborted) setBusy(false);
    }
  }
  async function readArchive() {
    if (!current) return;
    setError(undefined);
    archiveController.current?.abort();
    const request = new AbortController();
    archiveController.current = request;
    try {
      const d = await api<Definition>(
        `/api/v1/sla/definitions/${kind}/${current.id}?version=${archiveVersion}`,
        { signal: request.signal },
      );
      if (mounted.current && !request.signal.aborted) setArchive(d);
    } catch (cause) {
      if (mounted.current && !request.signal.aborted)
        setError(
          cause instanceof Error ? cause.message : "Błąd odczytu wersji",
        );
    }
  }
  return (
    <div className="panel">
      <UnsavedGuard
        includeSearch
        dirty={dirty || calendarDirty || busy || !!pending.current}
      />
      <Link
        to={`?kind=${kind}`}
        onClick={(e) => {
          if (dirty || busy || pending.current) {
            e.preventDefault();
            setError("Zapisz propozycję albo użyj przycisku Odrzuć edycję.");
          }
        }}
      >
        Wróć do listy
      </Link>
      <h2>
        {current
          ? `${current.config.name} — wersja ${current.version}`
          : kind === "calendar"
            ? "Nowy kalendarz"
            : "Nowa polityka SLA"}
      </h2>
      {error && (
        <p className="notice notice--error" role="alert">
          {error}
        </p>
      )}
      {notice && (
        <p className="notice" role="status">
          {notice}
        </p>
      )}
      {conflict && (
        <ConflictNotice
          current={<pre>{JSON.stringify(conflict.config, null, 2)}</pre>}
          proposal={<pre>{JSON.stringify(draft, null, 2)}</pre>}
          acceptCurrent={() => {
            setCurrent(conflict);
            setDraft(conflict.config);
            setCalendarDirty(false);
            setCalendarError(undefined);
            setConflict(undefined);
            setConflictBlocked(false);
            pending.current = undefined;
          }}
          keepProposal={() => {
            setCurrent(conflict);
            setConflict(undefined);
            setConflictBlocked(false);
            pending.current = undefined;
          }}
        />
      )}
      <form onSubmit={(e) => void save(e)}>
        <fieldset disabled={busy}>
          <label>
            Nazwa
            <input
              required
              maxLength={120}
              value={draft.name}
              onChange={(e) => update({ name: e.target.value })}
            />
          </label>
          {kind === "calendar" ? (
            <CalendarFields
              key={`${current?.version}:${editorRevision}`}
              value={draft as Calendar}
              update={update}
              changed={(error) => {
                setCalendarDirty(true);
                setCalendarError(error);
              }}
            />
          ) : (
            <PolicyFields
              value={draft as Policy}
              update={update}
              calendars={calendars.data?.items ?? []}
            />
          )}
          <div className="action-row">
            <button
              className="button button--primary"
              disabled={conflictBlocked || !!calendarError}
            >
              {busy ? "Zapisywanie…" : "Opublikuj wersję"}
            </button>
            {pending.current && (
              <button
                type="button"
                className="button button--secondary"
                onClick={() => void save(undefined, true)}
              >
                Sprawdź wynik poprzedniej próby
              </button>
            )}
            <button
              type="button"
              className="button button--secondary"
              onClick={() => {
                if (pending.current) {
                  setError("Najpierw sprawdź wynik poprzedniej próby zapisu.");
                  return;
                }
                setEditorRevision((v) => v + 1);
                setCalendarDirty(false);
                setCalendarError(undefined);
                setDraft(
                  current?.config ??
                    (kind === "calendar" ? initialCalendar : initialPolicy),
                );
                setError(undefined);
                setConflict(undefined);
                setConflictBlocked(false);
              }}
            >
              Odrzuć edycję
            </button>
          </div>
        </fieldset>
      </form>
      {kind === "policy" && calendars.error && (
        <p role="alert">
          Nie udało się odczytać kalendarzy: {calendars.error}
          <button onClick={calendars.reload}>Odśwież kalendarze</button>
        </p>
      )}
      {current && (
        <details>
          <summary>Archiwalna wersja i szczegóły techniczne</summary>
          <label>
            Numer wersji
            <input
              inputMode="numeric"
              pattern="[1-9][0-9]*"
              value={archiveVersion}
              onChange={(e) => setArchiveVersion(e.target.value)}
            />
          </label>
          <button
            type="button"
            disabled={!/^[1-9][0-9]*$/.test(archiveVersion)}
            onClick={() => void readArchive()}
          >
            Odczytaj wersję
          </button>
          {archive && <pre>{JSON.stringify(archive, null, 2)}</pre>}
          <p>
            Zmiany są audytowane pod obiektami sla_policy i sla_calendar.
            Incydent zachowuje identyfikatory użytych wersji.
          </p>
        </details>
      )}
    </div>
  );
}
function CalendarFields({
  value,
  update,
  changed,
}: {
  value: Calendar;
  update: (p: Partial<Calendar & Policy>) => void;
  changed: (error?: string) => void;
}) {
  const [hours, setHours] = useState(() =>
    Array.from({ length: 7 }, (_, weekday) =>
      windowsText(value.week.find((d) => d.weekday === weekday)?.windows ?? []),
    ),
  );
  const [holidays, setHolidays] = useState(value.holidays.join("\n"));
  const [exceptions, setExceptions] = useState(
    value.exceptions
      .map((e) => `${e.date}=${windowsText(e.windows)}`)
      .join("\n"),
  );
  const [error, setError] = useState<string>();
  function setCalendar(
    nextHours: string[],
    nextHolidays: string,
    nextExceptions: string,
  ) {
    try {
      const dates = nextHolidays
        .split(/\r?\n/)
        .map((s) => s.trim())
        .filter(Boolean);
      const overrides = nextExceptions
        .split(/\r?\n/)
        .map((s) => s.trim())
        .filter(Boolean)
        .map((s) => {
          const parts = s.split("=");
          if (parts.length !== 2)
            throw new Error(
              "Wyjątek wpisz jako 2026-12-24=09:00-12:00; puste godziny oznaczają dzień wolny.",
            );
          return { date: parts[0]!, windows: parseWindows(parts[1]!) };
        });
      update({
        week: nextHours.map((s, weekday) => ({
          weekday,
          windows: parseWindows(s),
        })),
        holidays: dates,
        exceptions: overrides,
      });
      setError(undefined);
      changed();
    } catch (cause) {
      const message = cause instanceof Error ? cause.message : "Błędne godziny";
      setError(message);
      changed(message);
    }
  }
  return (
    <>
      <label>
        Strefa IANA
        <input
          required
          value={value.timezone}
          onChange={(e) => update({ timezone: e.target.value })}
        />
      </label>
      <p>
        Godziny lokalne: 09:00-17:00; 18:00-20:00. Puste pole to dzień wolny.
        Przedziały nocne rozdziel na dwa dni. Niedziela zmiany czasu uwzględnia
        faktycznie istniejące minuty, w tym godzinę powtórzoną.
      </p>
      <div className="sla-hours">
        {[
          "Niedziela",
          "Poniedziałek",
          "Wtorek",
          "Środa",
          "Czwartek",
          "Piątek",
          "Sobota",
        ].map((day, d) => (
          <label key={day}>
            {day}
            <input
              value={hours[d]}
              onChange={(e) => {
                const next = [...hours];
                next[d] = e.target.value;
                setHours(next);
                setCalendar(next, holidays, exceptions);
              }}
            />
          </label>
        ))}
      </div>
      <label>
        Święta — lokalne daty, po jednej w wierszu
        <textarea
          value={holidays}
          onChange={(e) => {
            setHolidays(e.target.value);
            setCalendar(hours, e.target.value, exceptions);
          }}
        />
      </label>
      <label>
        Wyjątki — data=godziny, po jednym w wierszu
        <textarea
          value={exceptions}
          onChange={(e) => {
            setExceptions(e.target.value);
            setCalendar(hours, holidays, e.target.value);
          }}
        />
      </label>
      <p>
        Wyjątek ma pierwszeństwo przed świętem i tygodniem. Limit: 1000 świąt,
        1000 wyjątków, 8 przedziałów dziennie; co najmniej 60 minut pracy
        tygodniowo.
      </p>
      {error && <p role="alert">{error}</p>}
    </>
  );
}
function PolicyFields({
  value: p,
  update,
  calendars,
}: {
  value: Policy;
  update: (p: Partial<Calendar & Policy>) => void;
  calendars: Definition[];
}) {
  return (
    <>
      <label>
        <input
          type="checkbox"
          checked={p.enabled}
          onChange={(e) => update({ enabled: e.target.checked })}
        />
        Aktywna dla nowych incydentów
      </label>
      <div className="form-grid">
        <label>
          Kolejność wyboru
          <input
            type="number"
            min={0}
            max={100000}
            required
            value={p.order}
            onChange={(e) => update({ order: Number(e.target.value) })}
          />
        </label>
        <label>
          Priorytet
          <select
            aria-label="Priorytet"
            value={p.priority}
            onChange={(e) => update({ priority: Number(e.target.value) })}
          >
            <option value={0}>Dowolny</option>
            {[1, 2, 3, 4, 5].map((n) => (
              <option key={n} value={n}>
                P{n}
              </option>
            ))}
          </select>
        </label>
        <label>
          Kategoria
          <select
            aria-label="Kategoria"
            value={p.category}
            onChange={(e) => update({ category: e.target.value })}
          >
            {[
              ["", "Dowolna"],
              ["hardware", "Sprzęt"],
              ["software", "Oprogramowanie"],
              ["network", "Sieć"],
              ["access", "Dostęp"],
              ["other", "Inne"],
            ].map(([v, l]) => (
              <option key={v} value={v}>
                {l}
              </option>
            ))}
          </select>
        </label>
        <label>
          Wersja kalendarza
          <select
            aria-label="Wersja kalendarza"
            required
            value={
              p.calendar_id ? `${p.calendar_id}:${p.calendar_version}` : ""
            }
            onChange={(e) => {
              const [calendar_id, calendar_version] = e.target.value.split(":");
              update({ calendar_id, calendar_version });
            }}
          >
            <option value="">Wybierz kalendarz</option>
            {p.calendar_id &&
              !calendars.some(
                (c) =>
                  c.id === p.calendar_id && c.version === p.calendar_version,
              ) && (
                <option value={`${p.calendar_id}:${p.calendar_version}`}>
                  Kalendarz #{p.calendar_id} — przypięta wersja{" "}
                  {p.calendar_version}
                </option>
              )}
            {calendars.map((c) => (
              <option key={c.id} value={`${c.id}:${c.version}`}>
                {c.config.name} v{c.version}
              </option>
            ))}
          </select>
        </label>
        <label>
          Pierwsza odpowiedź — minuty robocze
          <input
            required
            type="number"
            min={1}
            max={43200}
            step={1}
            value={p.response_seconds / 60}
            onChange={(e) =>
              update({ response_seconds: Number(e.target.value) * 60 })
            }
          />
        </label>
        <label>
          Rozwiązanie — minuty robocze
          <input
            required
            type="number"
            min={1}
            max={43200}
            step={1}
            value={p.resolution_seconds / 60}
            onChange={(e) =>
              update({ resolution_seconds: Number(e.target.value) * 60 })
            }
          />
        </label>
        <label>
          Ostrzeżenie — procent celu
          <input
            required
            type="number"
            min={1}
            max={99}
            value={p.warning_percent}
            onChange={(e) =>
              update({ warning_percent: Number(e.target.value) })
            }
          />
        </label>
      </div>
      <label>
        Pauzujące powody wstrzymania — dokładna treść, po jednym w wierszu
        <textarea
          value={p.pause_reasons.join("\n")}
          onChange={(e) =>
            update({ pause_reasons: e.target.value.split("\n") })
          }
        />
      </label>
      <p>
        Start przy utworzeniu. Priorytet i kategoria pozostają z chwili wyboru.
        Ponowne otwarcie resetuje wyłącznie czas rozwiązania i zachowuje
        politykę oraz kalendarz. Zmiana kalendarza wymaga opublikowania polityki
        z nową wersją kalendarza. Ostrzeżenia i naruszenia korzystają z
        preferencji powiadomień „Zmiany stanu i SLA”.
      </p>
    </>
  );
}
