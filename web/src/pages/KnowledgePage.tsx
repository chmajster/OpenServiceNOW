import { flushSync } from "react-dom";
import { useState, type FormEvent } from "react";
import { Link, useNavigate, useParams, useSearchParams } from "react-router-dom";
import { api, ApiError, jsonBody } from "../api/client";
import { useAuth } from "../auth/AuthContext";
import { PageState } from "../components/PageState";
import { useRemotePage } from "../components/useRemotePage";
import { UnsavedGuard } from "../incidents/UnsavedGuard";
import { useIncidentQuery } from "../incidents/hooks";
import {
  articleNumber,
  pageStatus,
  type ArticleStatus,
  type KnowledgePageRecord,
  type PageSummary,
  type PageVersion,
  type Space,
} from "../knowledge/contracts";
import { SafeMarkdown } from "../knowledge/SafeMarkdown";

const base = "/api/v1/knowledge";

function errorMessage(cause: unknown, fallback: string) {
  return cause instanceof Error ? cause.message : fallback;
}

function dateTime(value?: string) {
  if (!value) return "";
  const date = new Date(value);
  const offset = date.getTimezoneOffset() * 60_000;
  return new Date(date.getTime() - offset).toISOString().slice(0, 16);
}

function isoDate(value: string) {
  return value ? new Date(value).toISOString() : undefined;
}

function statusClass(status: ArticleStatus) {
  if (status === "PUBLISHED") return "badge badge--success";
  if (status === "RETIRED") return "badge badge--muted";
  return "badge";
}

export function KnowledgePage() {
  const { hasPermission } = useAuth();
  const canRead = ["knowledge.read", "knowledge.write", "knowledge.manage"].some(hasPermission);
  if (!canRead)
    return <PageState title="Brak uprawnień" detail="Ta sekcja wymaga dostępu do bazy wiedzy." tone="error" />;
  return <KnowledgeWorkspace />;
}

function KnowledgeWorkspace() {
  const { spaceID, pageID } = useParams();
  const { hasPermission } = useAuth();
  const spaces = useRemotePage<Space>(`${base}/spaces?limit=100`, "spaces");
  if (!spaceID && spaces.loading && !spaces.items.length)
    return <PageState title="Ładowanie bazy wiedzy" detail="Pobieranie baz wiedzy i artykułów." />;
  if (!spaceID && spaces.error && !spaces.items.length)
    return <PageState title="Nie udało się otworzyć bazy wiedzy" detail={spaces.error} tone="error" />;
  if (!spaceID)
    return <KnowledgeHome spaces={spaces.items} canManage={hasPermission("knowledge.manage")} reload={spaces.reload} />;
  return <KnowledgeBase key={spaceID} spaceID={spaceID} pageID={pageID} spaces={spaces.items} reloadSpaces={spaces.reload} />;
}

function KnowledgeHome({ spaces, canManage, reload }: { spaces: Space[]; canManage: boolean; reload: () => void }) {
  const navigate = useNavigate();
  const [creating, setCreating] = useState(spaces.length === 0 && canManage);
  const [selected, setSelected] = useState(spaces[0]?.id ?? "");
  const [query, setQuery] = useState("");
  const articleCount = spaces.reduce((total, item) => total + BigInt(item.page_count), 0n).toString();
  function search(event: FormEvent) {
    event.preventDefault();
    if (selected && query.trim()) navigate(`/knowledge/${selected}?view=search&q=${encodeURIComponent(query.trim())}`);
  }
  return (
    <div className="page knowledge-page knowledge-home">
      <section className="knowledge-hero">
        <p className="eyebrow">Knowledge Management</p>
        <h1>Witamy w Bazie Wiedzy</h1>
        <p>Znajdź instrukcje, rozwiązania i zweryfikowane informacje operacyjne.</p>
        {spaces.length ? (
          <form className="knowledge-hero-search" onSubmit={search}>
            <select aria-label="Baza wiedzy" value={selected} onChange={(event) => setSelected(event.target.value)}>
              {spaces.map((space) => <option key={space.id} value={space.id}>{space.name}</option>)}
            </select>
            <label><span className="visually-hidden">Wyszukaj artykuły</span><input value={query} onChange={(event) => setQuery(event.target.value)} maxLength={200} placeholder="Wyszukaj w bazie wiedzy" /></label>
            <button className="button button--primary">Szukaj</button>
          </form>
        ) : null}
      </section>
      <div className="knowledge-home-summary" aria-label="Podsumowanie bazy wiedzy">
        <div><strong>{spaces.length}</strong><span>Bazy wiedzy</span></div>
        <div><strong>{articleCount}</strong><span>Artykuły</span></div>
      </div>
      <header className="knowledge-section-heading">
        <div><h2>Przeglądaj bazy wiedzy</h2><p className="muted">Wybierz obszar, aby wyświetlić dostępne artykuły.</p></div>
        {canManage ? <button className="button button--primary" onClick={() => setCreating((value) => !value)}>{creating ? "Anuluj" : "Nowa baza wiedzy"}</button> : null}
      </header>
      {creating ? <KnowledgeBaseCreate saved={() => { setCreating(false); reload(); }} /> : null}
      {!spaces.length && !creating ? <PageState title="Brak baz wiedzy" detail={canManage ? "Utwórz pierwszą bazę wiedzy." : "Administrator nie utworzył jeszcze bazy wiedzy."} /> : null}
      <div className="knowledge-base-grid">
        {spaces.map((space) => (
          <Link className="knowledge-base-card" key={space.id} to={`/knowledge/${space.id}`}>
            <span className="knowledge-base-icon" aria-hidden="true">KB</span>
            <div><h2>{space.name}</h2><p>{space.description || "Baza wiedzy bez opisu"}</p></div>
            <footer><span>{space.page_count} artykułów</span><span>{space.subscribed ? "Subskrybowana" : "Otwórz bazę"} →</span></footer>
          </Link>
        ))}
      </div>
    </div>
  );
}

function KnowledgeBaseCreate({ saved }: { saved: () => void }) {
  const [key, setKey] = useState("");
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [review, setReview] = useState(180);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  async function submit(event: FormEvent) {
    event.preventDefault(); setBusy(true); setError(undefined);
    try {
      await api<Space>(`${base}/spaces`, { method: "POST", headers: { "Idempotency-Key": crypto.randomUUID() }, body: jsonBody({ key, name, description, status: "ACTIVE", review_interval_days: review }) });
      saved();
    } catch (cause) { setError(errorMessage(cause, "Nie udało się utworzyć bazy wiedzy.")); }
    finally { setBusy(false); }
  }
  return (
    <form className="panel form-stack knowledge-base-form" onSubmit={(event) => void submit(event)}>
      <div className="panel-heading"><div><h2>Nowa baza wiedzy</h2><p className="muted">Oddziel artykuły według zespołu, usługi lub odbiorców.</p></div></div>
      <div className="form-grid">
        <label>Identyfikator<input required pattern="[A-Za-z][A-Za-z0-9]{1,15}" maxLength={16} value={key} onChange={(event) => setKey(event.target.value.toUpperCase())} placeholder="IT" /></label>
        <label>Nazwa<input required maxLength={160} value={name} onChange={(event) => setName(event.target.value)} placeholder="Wsparcie IT" /></label>
        <label>Domyślna ważność (dni)<input required type="number" min={0} max={3650} value={review} onChange={(event) => setReview(Number(event.target.value))} /></label>
      </div>
      <label>Opis<textarea rows={3} maxLength={10000} value={description} onChange={(event) => setDescription(event.target.value)} /></label>
      {error ? <p className="inline-error" role="alert">{error}</p> : null}
      <div className="action-row"><button className="button button--primary" disabled={busy}>{busy ? "Tworzenie…" : "Utwórz bazę wiedzy"}</button></div>
    </form>
  );
}

type ListView = "all" | "draft" | "published" | "retired" | "expiring" | "search" | "settings";

function availableView(value: string | null, canWrite: boolean, canManage: boolean): ListView {
  if (value === "search" || value === "published") return value;
  if (canWrite && (value === "draft" || value === "retired" || value === "expiring")) return value;
  if (canManage && value === "settings") return value;
  return "all";
}

function KnowledgeBase({ spaceID, pageID, spaces, reloadSpaces }: { spaceID: string; pageID?: string; spaces: Space[]; reloadSpaces: () => void }) {
  const navigate = useNavigate();
  const [params, setParams] = useSearchParams();
  const { hasPermission } = useAuth();
  const canWrite = hasPermission("knowledge.write") || hasPermission("knowledge.manage");
  const canManage = hasPermission("knowledge.manage");
  const space = useIncidentQuery<Space>(`${base}/spaces/${spaceID}`);
  const [spaceOverride, setSpaceOverride] = useState<Space>();
  const pages = useRemotePage<PageSummary>(`${base}/spaces/${spaceID}/pages?limit=100`, "pages");
  const [notice, setNotice] = useState<string>();
  const [actionError, setActionError] = useState<string>();
  const [busyAction, setBusyAction] = useState(false);
  const view = availableView(params.get("view"), canWrite, canManage);

  function showView(next: ListView) {
    const value = new URLSearchParams();
    if (next !== "all") value.set("view", next);
    if (pageID) navigate({ pathname: `/knowledge/${spaceID}`, search: value.toString() });
    else setParams(value);
  }

  async function subscribe() {
    if (!space.data) return;
    const current = spaceOverride ?? space.data;
    setBusyAction(true); setActionError(undefined); setNotice(undefined);
    try {
      const result = await api<Space>(`${base}/spaces/${spaceID}/subscription`, { method: current.subscribed ? "DELETE" : "PUT" });
      setSpaceOverride(result);
      setNotice(result.subscribed ? "Subskrybujesz tę bazę wiedzy." : "Anulowano subskrypcję bazy wiedzy.");
    } catch (cause) { setActionError(errorMessage(cause, "Nie udało się zmienić subskrypcji.")); }
    finally { setBusyAction(false); }
  }

  if (space.loading && !space.data) return <PageState title="Ładowanie bazy wiedzy" />;
  if (space.error || !space.data) return <PageState title="Nie udało się otworzyć bazy wiedzy" detail={space.error} tone="error" />;
  const current = spaceOverride ?? space.data;
  const counts = {
    all: pages.items.length,
    draft: pages.items.filter((item) => item.status === "DRAFT").length,
    published: pages.items.filter((item) => item.status === "PUBLISHED").length,
    retired: pages.items.filter((item) => item.status === "RETIRED").length,
  };
  return (
    <div className="page knowledge-page knowledge-center">
      <header className="knowledge-center-header">
        <div>
          <p className="eyebrow">Knowledge Center</p>
          <h1>{current.name}</h1>
          <p>{current.description || "Baza wiedzy"}</p>
        </div>
        <div className="knowledge-center-actions">
          <label><span className="visually-hidden">Zmień bazę wiedzy</span><select value={spaceID} onChange={(event) => navigate(`/knowledge/${event.target.value}`)}>{spaces.map((item) => <option value={item.id} key={item.id}>{item.name}</option>)}</select></label>
          <button className="button button--secondary" disabled={busyAction} onClick={() => void subscribe()}>{current.subscribed ? "Anuluj subskrypcję" : "Subskrybuj"}</button>
          {canWrite && current.status === "ACTIVE" ? <Link className="button button--primary" to={`/knowledge/${spaceID}/articles/new`}>Nowy artykuł</Link> : null}
        </div>
      </header>
      {current.status === "ARCHIVED" ? <p className="notice">Ta baza wiedzy jest zarchiwizowana i dostępna tylko do odczytu dla zarządzających.</p> : null}
      {notice ? <p className="inline-success" role="status">{notice}</p> : null}
      {actionError ? <p className="inline-error" role="alert">{actionError}</p> : null}
      <div className="knowledge-center-layout">
        <aside className="knowledge-module-nav" aria-label="Nawigacja Knowledge Center">
          <Link className="knowledge-module-home" to="/knowledge">← Wszystkie bazy</Link>
          <section><h2>Artykuły</h2>
            <ModuleButton label="Wszystkie" count={counts.all} active={!pageID && view === "all"} onClick={() => showView("all")} />
            {canWrite ? <ModuleButton label="Nieopublikowane" count={counts.draft} active={!pageID && view === "draft"} onClick={() => showView("draft")} /> : null}
            <ModuleButton label="Opublikowane" count={counts.published} active={!pageID && view === "published"} onClick={() => showView("published")} />
            {canWrite ? <ModuleButton label="Wycofane" count={counts.retired} active={!pageID && view === "retired"} onClick={() => showView("retired")} /> : null}
            {canWrite ? <ModuleButton label="Wygasające" active={!pageID && view === "expiring"} onClick={() => showView("expiring")} /> : null}
          </section>
          <section><h2>Narzędzia</h2>
            <ModuleButton label="Wyszukiwanie" active={!pageID && view === "search"} onClick={() => showView("search")} />
          </section>
          {canManage ? <section><h2>Administracja</h2><ModuleButton label="Konfiguracja bazy" active={!pageID && view === "settings"} onClick={() => showView("settings")} /></section> : null}
        </aside>
        <main className="knowledge-center-content">
          {pageID === "new" ? (
            <ArticleEditor space={current} saved={(record) => { pages.reload(); navigate(`/knowledge/${spaceID}/articles/${record.id}`); }} />
          ) : pageID ? (
            <ArticleDetails pageID={pageID} space={current} canWrite={canWrite && current.status === "ACTIVE"} changed={pages.reload} />
          ) : view === "search" ? (
            <KnowledgeSearch spaceID={spaceID} />
          ) : view === "settings" && canManage ? (
            <KnowledgeBaseSettings space={current} saved={(updated) => { setSpaceOverride(updated); reloadSpaces(); }} />
          ) : view === "expiring" && canWrite ? (
            <ExpiringArticles spaceID={spaceID} spaceName={current.name} />
          ) : (
            <ArticleDirectory spaceName={current.name} spaceID={spaceID} pages={pages.items} filter={view} loading={pages.loading} error={pages.error} retry={pages.retry} />
          )}
        </main>
      </div>
    </div>
  );
}

function ModuleButton({ label, count, active, onClick }: { label: string; count?: number; active: boolean; onClick: () => void }) {
  return <button aria-current={active ? "page" : undefined} onClick={onClick}><span>{label}</span>{count !== undefined ? <small>{count}</small> : null}</button>;
}

function ArticleDirectory({ spaceName, spaceID, pages, filter, loading, error, retry, showHeading = true }: { spaceName: string; spaceID: string; pages: PageSummary[]; filter: ListView; loading: boolean; error: string | null; retry: () => void; showHeading?: boolean }) {
  const visible = filter === "all" ? pages : pages.filter((page) => page.status.toLowerCase() === filter);
  if (loading && !pages.length) return <PageState title="Ładowanie artykułów" />;
  if (error) return <div className="form-stack"><PageState title="Nie udało się pobrać artykułów" detail={error} tone="error" /><button className="button button--secondary" onClick={() => void retry()}>Spróbuj ponownie</button></div>;
  return (
    <section className="knowledge-list-view">
      {showHeading ? <header className="knowledge-list-heading"><div><p className="eyebrow">Artykuły</p><h2>{filter === "all" ? "Wszystkie" : pageStatus[filter.toUpperCase() as ArticleStatus]}</h2></div><span>{visible.length} wyników</span></header> : null}
      {!visible.length ? <PageState title="Brak artykułów" detail="W tym widoku nie ma artykułów spełniających kryteria." /> : (
        <div className="knowledge-table-wrap"><table className="knowledge-table"><thead><tr><th>Numer</th><th>Krótki opis</th><th>Baza wiedzy</th><th>Stan</th><th>Ważny do</th><th>Zaktualizowano</th></tr></thead><tbody>
          {visible.map((page) => <tr key={page.id}><td data-label="Numer"><Link to={`/knowledge/${spaceID}/articles/${page.id}`}>{articleNumber(page.id)}</Link></td><td data-label="Krótki opis"><Link to={`/knowledge/${spaceID}/articles/${page.id}`}>{page.title}</Link></td><td data-label="Baza wiedzy">{spaceName}</td><td data-label="Stan"><span className={statusClass(page.status)}>{pageStatus[page.status]}</span></td><td data-label="Ważny do">{page.review_due_at ? new Date(page.review_due_at).toLocaleDateString("pl-PL") : "—"}</td><td data-label="Zaktualizowano">{new Date(page.updated_at).toLocaleDateString("pl-PL")}</td></tr>)}
        </tbody></table></div>
      )}
    </section>
  );
}

function KnowledgeSearch({ spaceID }: { spaceID: string }) {
  const [params, setParams] = useSearchParams();
  const query = params.get("q") ?? "";
  const [value, setValue] = useState(query);
  const result = useRemotePage<PageSummary>(`${base}/spaces/${spaceID}/search?limit=100&q=${encodeURIComponent(query)}`, "pages", "after_id", query.trim().length > 0);
  return (
    <section className="form-stack"><header className="knowledge-list-heading"><div><p className="eyebrow">Artykuły</p><h2>Wyszukiwanie</h2></div></header>
      <form className="knowledge-search" onSubmit={(event) => { event.preventDefault(); const next = new URLSearchParams({ view: "search" }); if (value.trim()) next.set("q", value.trim()); setParams(next); }}>
        <label>Wyszukaj w tytule i treści<input value={value} maxLength={200} onChange={(event) => setValue(event.target.value)} placeholder="Wpisz co najmniej jedno słowo" /></label><button className="button button--primary">Szukaj</button>
      </form>
      {!query ? <PageState title="Wyszukaj artykuł" detail="Wyniki uwzględniają artykuły dostępne dla Twojej roli." /> : result.loading ? <PageState title="Wyszukiwanie" /> : result.error ? <PageState title="Wyszukiwanie nie powiodło się" detail={result.error} tone="error" /> : !result.items.length ? <PageState title="Brak wyników" detail={`Nie znaleziono artykułów dla „${query}”.`} /> : <ArticleSearchResults spaceID={spaceID} pages={result.items} />}
    </section>
  );
}

function ExpiringArticles({ spaceID, spaceName }: { spaceID: string; spaceName: string }) {
  const result = useRemotePage<PageSummary>(`${base}/spaces/${spaceID}/attention?limit=100`, "pages");
  if (result.loading) return <PageState title="Sprawdzanie ważności artykułów" />;
  if (result.error) return <PageState title="Nie udało się pobrać wygasających artykułów" detail={result.error} tone="error" />;
  const expired = result.items.filter((item) => item.attention_reason === "REVIEW_DUE");
  return <section><header className="knowledge-list-heading"><div><p className="eyebrow">Artykuły</p><h2>Wygasające artykuły</h2><p className="muted">Opublikowane artykuły po dacie ważności wymagają ponownej weryfikacji.</p></div><span>{expired.length} wyników</span></header>{expired.length ? <ArticleDirectory spaceName={spaceName} spaceID={spaceID} pages={expired} filter="all" loading={false} error={null} retry={result.retry} showHeading={false} /> : <PageState title="Brak wygasających artykułów" detail="Wszystkie opublikowane artykuły mają aktualną datę ważności." tone="success" />}</section>;
}

function ArticleSearchResults({ spaceID, pages }: { spaceID: string; pages: PageSummary[] }) {
  return <div className="knowledge-search-results">{pages.map((page) => <Link className="panel" key={page.id} to={`/knowledge/${spaceID}/articles/${page.id}`}><div><small>{articleNumber(page.id)}</small><h3>{page.title}</h3><p>{page.excerpt || "Brak podglądu treści"}</p></div><span className={statusClass(page.status)}>{pageStatus[page.status]}</span></Link>)}</div>;
}

function KnowledgeBaseSettings({ space, saved }: { space: Space; saved: (space: Space) => void }) {
  const [draft, setDraft] = useState({ key: space.key, name: space.name, description: space.description, status: space.status, review_interval_days: space.review_interval_days });
  const [busy, setBusy] = useState(false); const [error, setError] = useState<string>(); const [notice, setNotice] = useState<string>();
  async function submit(event: FormEvent) {
    event.preventDefault(); setBusy(true); setError(undefined); setNotice(undefined);
    try { const updated = await api<Space>(`${base}/spaces/${space.id}`, { method: "PUT", body: jsonBody({ ...draft, version: space.version }) }); setNotice("Konfiguracja bazy wiedzy została zapisana."); saved(updated); }
    catch (cause) { setError(errorMessage(cause, "Nie udało się zapisać konfiguracji.")); }
    finally { setBusy(false); }
  }
  return <form className="panel form-stack" onSubmit={(event) => void submit(event)}><div><p className="eyebrow">Administracja</p><h2>Konfiguracja bazy wiedzy</h2><p className="muted">Archiwizacja ukrywa bazę przed czytelnikami i blokuje edycję artykułów.</p></div><div className="form-grid"><label>Identyfikator<input required pattern="[A-Za-z][A-Za-z0-9]{1,15}" value={draft.key} onChange={(event) => setDraft({ ...draft, key: event.target.value.toUpperCase() })} /></label><label>Nazwa<input required maxLength={160} value={draft.name} onChange={(event) => setDraft({ ...draft, name: event.target.value })} /></label><label>Stan<select value={draft.status} onChange={(event) => setDraft({ ...draft, status: event.target.value as Space["status"] })}><option value="ACTIVE">Aktywna</option><option value="ARCHIVED">Zarchiwizowana</option></select></label><label>Domyślna ważność (dni)<input type="number" min={0} max={3650} value={draft.review_interval_days} onChange={(event) => setDraft({ ...draft, review_interval_days: Number(event.target.value) })} /></label></div><label>Opis<textarea rows={4} maxLength={10000} value={draft.description} onChange={(event) => setDraft({ ...draft, description: event.target.value })} /></label>{error ? <p className="inline-error" role="alert">{error}</p> : null}{notice ? <p className="inline-success" role="status">{notice}</p> : null}<div><button className="button button--primary" disabled={busy}>{busy ? "Zapisywanie…" : "Zapisz konfigurację"}</button></div></form>;
}

function ArticleDetails({ pageID, space, canWrite, changed }: { pageID: string; space: Space; canWrite: boolean; changed: () => void }) {
  const detail = useIncidentQuery<KnowledgePageRecord>(`${base}/pages/${pageID}`);
  const versions = useIncidentQuery<{ versions: PageVersion[] }>(`${base}/pages/${pageID}/versions?limit=20`);
  const [editing, setEditing] = useState(false);
  if (detail.loading && !detail.data) return <PageState title="Ładowanie artykułu" />;
  if (detail.error || !detail.data) return <PageState title="Nie udało się otworzyć artykułu" detail={detail.error} tone="error" />;
  const page = detail.data;
  if (editing) return <ArticleEditor space={space} record={page} saved={() => { setEditing(false); detail.reload(); versions.reload(); changed(); }} cancel={() => setEditing(false)} />;
  return <article className="knowledge-article"><nav className="knowledge-breadcrumbs" aria-label="Okruszki"><Link to="/knowledge">Baza wiedzy</Link><span>/</span><Link to={`/knowledge/${space.id}`}>{space.name}</Link><span>/</span><span>{articleNumber(page.id)}</span></nav><header className="knowledge-article-heading"><div><div className="action-row"><span className={statusClass(page.status)}>{pageStatus[page.status]}</span><span className="knowledge-article-number">{articleNumber(page.id)}</span></div><h2>{page.title}</h2><p className="muted">Wersja {page.version} · zaktualizowano {new Date(page.updated_at).toLocaleString("pl-PL")} przez {page.updated_by_name || `#${page.updated_by}`}</p>{page.review_due_at ? <p className="muted">Ważny do: {new Date(page.review_due_at).toLocaleDateString("pl-PL")}</p> : null}</div>{canWrite ? <button className="button button--secondary" onClick={() => setEditing(true)}>Edytuj artykuł</button> : null}</header><SafeMarkdown source={page.body} /><aside className="knowledge-history"><h3>Wersje artykułu</h3>{versions.loading ? <p className="muted">Ładowanie historii…</p> : versions.error ? <p className="inline-error">{versions.error}</p> : <ol>{versions.data?.versions.map((version) => <li key={version.version}><strong>Wersja {version.version}</strong><span>{pageStatus[version.status]} · {new Date(version.changed_at).toLocaleString("pl-PL")} · {version.changed_by_name || `#${version.changed_by}`}</span></li>)}</ol>}</aside></article>;
}

function ArticleEditor({ space, record, saved, cancel }: { space: Space; record?: KnowledgePageRecord; saved: (page: KnowledgePageRecord) => void; cancel?: () => void }) {
  const [draft, setDraft] = useState<{ title: string; body: string; status: ArticleStatus; review_due_at: string }>({ title: record?.title ?? "", body: record?.body ?? "", status: record?.status ?? "DRAFT", review_due_at: dateTime(record?.review_due_at) });
  const [currentVersion, setCurrentVersion] = useState(record?.version);
  const [busy, setBusy] = useState(false); const [dirty, setDirty] = useState(false); const [error, setError] = useState<string>(); const [conflict, setConflict] = useState(false);
  function update(value: Partial<typeof draft>) { setDraft((current) => ({ ...current, ...value })); setDirty(true); setConflict(false); }
  async function submit(event: FormEvent) {
    event.preventDefault(); setBusy(true); setError(undefined);
    const body = { title: draft.title, body: draft.body, status: draft.status, review_due_at: isoDate(draft.review_due_at), ...(currentVersion ? { version: currentVersion } : {}) };
    try {
      const result = await api<KnowledgePageRecord>(record ? `${base}/pages/${record.id}` : `${base}/spaces/${space.id}/pages`, { method: record ? "PUT" : "POST", headers: record ? undefined : { "Idempotency-Key": crypto.randomUUID() }, body: jsonBody(body) });
      flushSync(() => setDirty(false)); saved(result);
    } catch (cause) { if (cause instanceof ApiError && cause.status === 409) setConflict(true); setError(errorMessage(cause, "Nie udało się zapisać artykułu.")); }
    finally { setBusy(false); }
  }
  async function loadLatest() {
    if (!record) return;
    setBusy(true); setError(undefined);
    try { const latest = await api<KnowledgePageRecord>(`${base}/pages/${record.id}`); setDraft({ title: latest.title, body: latest.body, status: latest.status, review_due_at: dateTime(latest.review_due_at) }); setCurrentVersion(latest.version); setDirty(false); setConflict(false); }
    catch (cause) { setError(errorMessage(cause, "Nie udało się wczytać aktualnego artykułu.")); }
    finally { setBusy(false); }
  }
  return <form className="knowledge-editor form-stack" onSubmit={(event) => void submit(event)}><UnsavedGuard dirty={dirty} /><header className="panel-heading"><div><p className="eyebrow">{record ? articleNumber(record.id) : "Nowy rekord"}</p><h2>{record ? "Edytuj artykuł wiedzy" : "Nowy artykuł wiedzy"}</h2></div><div className="action-row">{cancel ? <button type="button" className="button button--secondary" onClick={cancel}>Anuluj</button> : <Link className="button button--secondary" to={`/knowledge/${space.id}`}>Anuluj</Link>}<button className="button button--primary" disabled={busy}>{busy ? "Zapisywanie…" : "Zapisz artykuł"}</button></div></header>{error ? <p className="inline-error" role="alert">{error}</p> : null}{conflict ? <div className="notice notice--error"><strong>Artykuł ma nowszą wersję.</strong><p>Skopiuj potrzebne zmiany, a następnie wczytaj aktualną wersję przed ponowną edycją.</p><button type="button" className="button button--secondary" disabled={busy} onClick={() => void loadLatest()}>Wczytaj aktualną wersję</button></div> : null}<div className="knowledge-editor-meta"><label>Krótki opis<input required maxLength={200} value={draft.title} onChange={(event) => update({ title: event.target.value })} /></label><label>Stan<select value={draft.status} onChange={(event) => update({ status: event.target.value as ArticleStatus })}><option value="DRAFT">Nieopublikowany</option><option value="PUBLISHED">Opublikowany</option><option value="RETIRED">Wycofany</option></select></label>{draft.status === "PUBLISHED" ? <label>Ważny do<input type="datetime-local" value={draft.review_due_at} onChange={(event) => update({ review_due_at: event.target.value })} /></label> : null}</div><div className="knowledge-editor-columns"><label>Treść artykułu<textarea required rows={24} maxLength={262144} value={draft.body} onChange={(event) => update({ body: event.target.value })} spellCheck /></label><div><span className="knowledge-preview-label">Podgląd artykułu</span><SafeMarkdown source={draft.body} /></div></div></form>;
}
