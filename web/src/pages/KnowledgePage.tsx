import {
  useMemo,
  useRef,
  useState,
  type ChangeEvent,
  type FormEvent,
} from "react";
import { Link, useNavigate, useParams, useSearchParams } from "react-router-dom";
import { flushSync } from "react-dom";
import { api, ApiError, download, jsonBody, upload } from "../api/client";
import { useAuth } from "../auth/AuthContext";
import { PageState } from "../components/PageState";
import { useRemotePage } from "../components/useRemotePage";
import { UnsavedGuard } from "../incidents/UnsavedGuard";
import { useIncidentQuery } from "../incidents/hooks";
import {
  attentionLabel,
  pageStatus,
  type ImportResult,
  type KnowledgePageRecord,
  type PageSummary,
  type PageVersion,
  type Space,
} from "../knowledge/contracts";
import { SafeMarkdown } from "../knowledge/SafeMarkdown";
import { knowledgeTemplates } from "../knowledge/templates";

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

export function KnowledgePage() {
  const { hasPermission } = useAuth();
  const canRead = ["knowledge.read", "knowledge.write", "knowledge.manage"].some(hasPermission);
  if (!canRead)
    return <PageState title="Brak uprawnień" detail="Ta sekcja wymaga dostępu do dokumentacji." tone="error" />;
  return <KnowledgeWorkspace />;
}

function KnowledgeWorkspace() {
  const { spaceID, pageID } = useParams();
  const { hasPermission } = useAuth();
  const spaces = useRemotePage<Space>(`${base}/spaces?limit=100`, "spaces");
  if (!spaceID && spaces.loading && !spaces.items.length) return <PageState title="Ładowanie dokumentacji" detail="Pobieranie przestrzeni i stron." />;
  if (!spaceID && spaces.error && !spaces.items.length)
    return <PageState title="Nie udało się otworzyć dokumentacji" detail={spaces.error} tone="error" />;
  if (!spaceID)
    return <SpaceDirectory spaces={spaces.items} canManage={hasPermission("knowledge.manage")} reload={spaces.reload} />;
  return (
    <SpaceWorkspace
      key={spaceID}
      spaceID={spaceID}
      pageID={pageID}
      spaces={spaces.items}
      reloadSpaces={spaces.reload}
    />
  );
}

function SpaceDirectory({ spaces, canManage, reload }: { spaces: Space[]; canManage: boolean; reload: () => void }) {
  const [creating, setCreating] = useState(spaces.length === 0 && canManage);
  return (
    <div className="page knowledge-page">
      <header className="page-heading">
        <div>
          <p className="eyebrow">Baza wiedzy</p>
          <h1>Dokumentacja</h1>
          <p className="muted">Procedury, decyzje i wiedza operacyjna zespołu w uporządkowanych przestrzeniach.</p>
        </div>
        {canManage ? <button className="button button--primary" onClick={() => setCreating((value) => !value)}>{creating ? "Anuluj" : "Nowa przestrzeń"}</button> : null}
      </header>
      {creating ? <SpaceCreate saved={() => { setCreating(false); reload(); }} /> : null}
      {!spaces.length && !creating ? <PageState title="Brak przestrzeni" detail={canManage ? "Utwórz pierwszą przestrzeń dokumentacji." : "Administrator nie utworzył jeszcze przestrzeni."} /> : null}
      <div className="knowledge-space-grid">
        {spaces.map((space) => (
          <Link className="knowledge-space-card" key={space.id} to={`/knowledge/${space.id}`}>
            <span className="knowledge-space-key">{space.key}</span>
            <div><h2>{space.name}</h2><p>{space.description || "Bez opisu"}</p></div>
            <small>{space.page_count} stron · {space.status === "ARCHIVED" ? "Zarchiwizowana" : "Aktywna"}</small>
          </Link>
        ))}
      </div>
    </div>
  );
}

function SpaceCreate({ saved }: { saved: () => void }) {
  const [key, setKey] = useState("");
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [review, setReview] = useState(180);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  async function submit(event: FormEvent) {
    event.preventDefault();
    setBusy(true); setError(undefined);
    try {
      await api<Space>(`${base}/spaces`, {
        method: "POST",
        headers: { "Idempotency-Key": crypto.randomUUID() },
        body: jsonBody({ key, name, description, status: "ACTIVE", review_interval_days: review }),
      });
      saved();
    } catch (cause) { setError(errorMessage(cause, "Nie udało się utworzyć przestrzeni.")); }
    finally { setBusy(false); }
  }
  return (
    <form className="panel form-stack" onSubmit={(event) => void submit(event)}>
      <div className="panel-heading"><div><h2>Nowa przestrzeń</h2><p className="muted">Klucz będzie krótkim, unikalnym oznaczeniem przestrzeni.</p></div></div>
      <div className="form-grid">
        <label>Klucz<input required pattern="[A-Za-z][A-Za-z0-9]{1,15}" maxLength={16} value={key} onChange={(e) => setKey(e.target.value.toUpperCase())} placeholder="OPS" /></label>
        <label>Nazwa<input required maxLength={160} value={name} onChange={(e) => setName(e.target.value)} placeholder="Operacje IT" /></label>
        <label>Przegląd co ile dni<input required type="number" min={0} max={3650} value={review} onChange={(e) => setReview(Number(e.target.value))} /></label>
      </div>
      <label>Opis<textarea rows={3} maxLength={10000} value={description} onChange={(e) => setDescription(e.target.value)} /></label>
      {error ? <p className="inline-error" role="alert">{error}</p> : null}
      <div className="action-row"><button className="button button--primary" disabled={busy}>{busy ? "Tworzenie…" : "Utwórz przestrzeń"}</button></div>
    </form>
  );
}

function SpaceWorkspace({ spaceID, pageID, spaces, reloadSpaces }: { spaceID: string; pageID?: string; spaces: Space[]; reloadSpaces: () => void }) {
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
  const [uploadProgress, setUploadProgress] = useState<number>();
  const fileRef = useRef<HTMLInputElement>(null);
  const view = params.get("view") ?? "pages";

  function showView(next: string) {
    const value = new URLSearchParams();
    if (next !== "pages") value.set("view", next);
    if (pageID) {
      navigate({ pathname: `/knowledge/${spaceID}`, search: value.toString() });
    } else {
      setParams(value);
    }
  }

  async function watch() {
    if (!space.data) return;
    setBusyAction(true); setActionError(undefined); setNotice(undefined);
    try {
      const result = await api<Space>(`${base}/spaces/${spaceID}/watch`, { method: current.watched ? "DELETE" : "PUT" });
      setNotice(result.watched ? "Obserwujesz tę przestrzeń." : "Wyłączono obserwowanie przestrzeni.");
      setSpaceOverride(result);
    } catch (cause) { setActionError(errorMessage(cause, "Nie udało się zmienić obserwowania.")); }
    finally { setBusyAction(false); }
  }

  async function exportArchive() {
    if (!space.data) return;
    setBusyAction(true); setActionError(undefined);
    try { await download(`${base}/spaces/${spaceID}/export`, `opensnow-${space.data.key.toLowerCase()}-documentation.zip`); setNotice("Eksport został pobrany."); }
    catch (cause) { setActionError(errorMessage(cause, "Nie udało się wyeksportować przestrzeni.")); }
    finally { setBusyAction(false); }
  }

  async function importArchive(event: ChangeEvent<HTMLInputElement>) {
    const file = event.target.files?.[0];
    event.target.value = "";
    if (!file) return;
    setBusyAction(true); setUploadProgress(0); setActionError(undefined); setNotice(undefined);
    const form = new FormData(); form.set("file", file);
    try {
      const result = await upload<ImportResult>(`${base}/spaces/${spaceID}/import`, form, crypto.randomUUID(), setUploadProgress);
      setNotice(result.replayed ? "Ten import był już wykonany." : `Zaimportowano ${result.created} stron.`);
      pages.reload();
    } catch (cause) { setActionError(errorMessage(cause, "Nie udało się zaimportować archiwum.")); }
    finally { setBusyAction(false); setUploadProgress(undefined); }
  }

  if (space.loading && !space.data) return <PageState title="Ładowanie przestrzeni" />;
  if (space.error || !space.data) return <PageState title="Nie udało się otworzyć przestrzeni" detail={space.error} tone="error" />;
  const current = spaceOverride ?? space.data;
  return (
    <div className="page knowledge-page">
      <header className="knowledge-header">
        <div className="knowledge-space-title">
          <span className="knowledge-space-key">{current.key}</span>
          <div><h1>{current.name}</h1><p>{current.description || "Przestrzeń dokumentacji"}</p></div>
        </div>
        <div className="knowledge-header-actions">
          <select aria-label="Zmień przestrzeń" value={spaceID} onChange={(e) => navigate(`/knowledge/${e.target.value}`)}>
            {spaces.map((item) => <option value={item.id} key={item.id}>{item.name}</option>)}
          </select>
          <details className="knowledge-actions">
            <summary className="button button--secondary">Więcej działań</summary>
            <div className="knowledge-actions-menu" role="menu" onClick={(event) => { if ((event.target as HTMLElement).closest("button")) event.currentTarget.closest("details")?.removeAttribute("open"); }}>
              {canManage ? <button role="menuitem" onClick={() => showView("settings")}>Ustawienia przestrzeni</button> : null}
              {canWrite ? <button role="menuitem" onClick={() => showView("attention")}>Dokumenty wymagające uwagi</button> : null}
              <button role="menuitem" disabled={busyAction} onClick={() => void watch()}>{current.watched ? "Przestań obserwować" : "Obserwuj przestrzeń"}</button>
              {canManage ? <button role="menuitem" disabled={busyAction} onClick={() => void exportArchive()}>Eksport ZIP</button> : null}
              {canManage ? <button role="menuitem" disabled={busyAction} onClick={() => fileRef.current?.click()}>Import</button> : null}
            </div>
          </details>
          {canManage ? <input ref={fileRef} className="visually-hidden" type="file" accept=".zip,application/zip" onChange={(event) => void importArchive(event)} /> : null}
          {canWrite ? <Link className="button button--primary" to={`/knowledge/${spaceID}/new`}>Utwórz stronę</Link> : null}
        </div>
      </header>
      {current.status === "ARCHIVED" ? <p className="notice">Ta przestrzeń jest zarchiwizowana i dostępna tylko do odczytu dla zarządzających.</p> : null}
      {notice ? <p className="inline-success" role="status">{notice}</p> : null}
      {actionError ? <p className="inline-error" role="alert">{actionError}</p> : null}
      {uploadProgress !== undefined ? <p className="muted" role="status">Wysyłanie archiwum: {uploadProgress}%</p> : null}
      <nav className="knowledge-tabs" aria-label="Dokumentacja">
        <button aria-current={view === "pages" ? "page" : undefined} onClick={() => showView("pages")}>Strony</button>
        <button aria-current={view === "templates" ? "page" : undefined} onClick={() => showView("templates")}>Szablony</button>
        <button aria-current={view === "search" ? "page" : undefined} onClick={() => showView("search")}>Szukaj</button>
      </nav>
      {pageID === "new" ? (
        <PageEditor space={current} pages={pages.items} saved={(record) => { pages.reload(); navigate(`/knowledge/${spaceID}/pages/${record.id}`); }} />
      ) : pageID ? (
        <PageDetails pageID={pageID} space={current} pages={pages.items} canWrite={canWrite && current.status === "ACTIVE"} changed={pages.reload} />
      ) : view === "templates" ? (
        <Templates spaceID={spaceID} canWrite={canWrite && current.status === "ACTIVE"} />
      ) : view === "search" ? (
        <Search spaceID={spaceID} />
      ) : view === "settings" && canManage ? (
        <SpaceSettings space={current} saved={(updated) => { setSpaceOverride(updated); reloadSpaces(); }} />
      ) : view === "attention" && canWrite ? (
        <Attention spaceID={spaceID} />
      ) : (
        <PageDirectory spaceID={spaceID} pages={pages.items} loading={pages.loading} error={pages.error} retry={pages.retry} />
      )}
    </div>
  );
}

function PageDirectory({ spaceID, pages, loading, error, retry }: { spaceID: string; pages: PageSummary[]; loading: boolean; error: string | null; retry: () => void }) {
  const depths = useMemo(() => {
    const byID = new Map(pages.map((page) => [page.id, page]));
    const result = new Map<string, number>();
    const depth = (page: PageSummary) => {
      let value = 0, parent = page.parent_id, guard = 0;
      while (parent && byID.has(parent) && guard++ < pages.length) { value += 1; parent = byID.get(parent)?.parent_id; }
      return value;
    };
    pages.forEach((page) => result.set(page.id, depth(page)));
    return result;
  }, [pages]);
  if (loading && !pages.length) return <PageState title="Ładowanie stron" />;
  if (error) return <div className="form-stack"><PageState title="Nie udało się pobrać stron" detail={error} tone="error" /><button className="button button--secondary" onClick={() => void retry()}>Spróbuj ponownie</button></div>;
  if (!pages.length) return <PageState title="Ta przestrzeń jest pusta" detail="Utwórz pierwszą stronę lub skorzystaj z gotowego szablonu." />;
  return (
    <section className="panel knowledge-page-list" aria-label="Strony przestrzeni">
      {pages.map((page) => (
        <Link key={page.id} to={`/knowledge/${spaceID}/pages/${page.id}`} style={{ paddingLeft: `${1 + (depths.get(page.id) ?? 0) * 1.35}rem` }}>
          <span className="knowledge-page-icon" aria-hidden="true">▤</span>
          <span><strong>{page.title}</strong><small>{pageStatus[page.status]} · zaktualizowano {new Date(page.updated_at).toLocaleString("pl-PL")}</small></span>
          {page.status === "DRAFT" ? <span className="badge">Szkic</span> : null}
        </Link>
      ))}
    </section>
  );
}

function Templates({ spaceID, canWrite }: { spaceID: string; canWrite: boolean }) {
  return (
    <section><div className="panel-heading"><div><h2>Szablony</h2><p className="muted">Zacznij od sprawdzonej struktury i dostosuj ją przed zapisaniem.</p></div></div>
      <div className="knowledge-template-grid">
        {knowledgeTemplates.map((template) => <article className="panel" key={template.id}><span className="knowledge-template-icon">◇</span><h2>{template.name}</h2><p>{template.description}</p>{canWrite ? <Link className="button button--secondary" to={`/knowledge/${spaceID}/new?template=${template.id}`}>Użyj szablonu</Link> : null}</article>)}
      </div>
    </section>
  );
}

function Search({ spaceID }: { spaceID: string }) {
  const [params, setParams] = useSearchParams();
  const query = params.get("q") ?? "";
  const [value, setValue] = useState(query);
  const result = useRemotePage<PageSummary>(`${base}/spaces/${spaceID}/search?limit=100&q=${encodeURIComponent(query)}`, "pages", "after_id", query.trim().length > 0);
  return (
    <section className="form-stack">
      <form className="knowledge-search" onSubmit={(event) => { event.preventDefault(); const next = new URLSearchParams({ view: "search" }); if (value.trim()) next.set("q", value.trim()); setParams(next); }}>
        <label>Przeszukaj tytuły i treść<input value={value} maxLength={200} onChange={(e) => setValue(e.target.value)} placeholder="Np. wdrożenie produkcyjne" /></label>
        <button className="button button--primary">Szukaj</button>
      </form>
      {!query ? <PageState title="Wpisz szukaną frazę" detail="Wyniki obejmą opublikowane strony oraz szkice dostępne dla Twojej roli." /> : result.loading ? <PageState title="Wyszukiwanie" /> : result.error ? <PageState title="Wyszukiwanie nie powiodło się" detail={result.error} tone="error" /> : !result.items.length ? <PageState title="Brak wyników" detail={`Nie znaleziono stron dla „${query}”.`} /> : <ResultList spaceID={spaceID} pages={result.items} />}
    </section>
  );
}

function Attention({ spaceID }: { spaceID: string }) {
  const result = useRemotePage<PageSummary>(`${base}/spaces/${spaceID}/attention?limit=100`, "pages");
  if (result.loading) return <PageState title="Sprawdzanie dokumentów" />;
  if (result.error) return <PageState title="Nie udało się pobrać dokumentów" detail={result.error} tone="error" />;
  return <section><div className="panel-heading"><div><h2>Dokumenty wymagające uwagi</h2><p className="muted">Szkice i opublikowane strony po terminie przeglądu.</p></div></div>{result.items.length ? <ResultList spaceID={spaceID} pages={result.items} attention /> : <PageState title="Wszystko aktualne" detail="Brak szkiców i zaległych przeglądów." tone="success" />}</section>;
}

function ResultList({ spaceID, pages, attention = false }: { spaceID: string; pages: PageSummary[]; attention?: boolean }) {
  return <div className="knowledge-results">{pages.map((page) => <Link className="panel" key={page.id} to={`/knowledge/${spaceID}/pages/${page.id}`}><div><h2>{page.title}</h2><p>{page.excerpt || "Brak podglądu treści"}</p></div><small>{attention ? attentionLabel[page.attention_reason ?? ""] : pageStatus[page.status]} · {new Date(page.updated_at).toLocaleString("pl-PL")}</small></Link>)}</div>;
}

function SpaceSettings({ space, saved }: { space: Space; saved: (space: Space) => void }) {
  const [draft, setDraft] = useState({ key: space.key, name: space.name, description: space.description, status: space.status, review_interval_days: space.review_interval_days });
  const [busy, setBusy] = useState(false); const [error, setError] = useState<string>(); const [notice, setNotice] = useState<string>();
  async function submit(event: FormEvent) {
    event.preventDefault(); setBusy(true); setError(undefined); setNotice(undefined);
    try { const updated = await api<Space>(`${base}/spaces/${space.id}`, { method: "PUT", body: jsonBody({ ...draft, version: space.version }) }); setNotice("Ustawienia przestrzeni zapisano."); saved(updated); }
    catch (cause) { setError(errorMessage(cause, "Nie udało się zapisać ustawień.")); }
    finally { setBusy(false); }
  }
  return <form className="panel form-stack" onSubmit={(event) => void submit(event)}><div><h2>Ustawienia przestrzeni</h2><p className="muted">Archiwizacja ukrywa przestrzeń przed czytelnikami i blokuje edycję stron.</p></div><div className="form-grid"><label>Klucz<input required pattern="[A-Za-z][A-Za-z0-9]{1,15}" value={draft.key} onChange={(e) => setDraft({ ...draft, key: e.target.value.toUpperCase() })} /></label><label>Nazwa<input required maxLength={160} value={draft.name} onChange={(e) => setDraft({ ...draft, name: e.target.value })} /></label><label>Stan<select value={draft.status} onChange={(e) => setDraft({ ...draft, status: e.target.value as Space["status"] })}><option value="ACTIVE">Aktywna</option><option value="ARCHIVED">Zarchiwizowana</option></select></label><label>Cykl przeglądu (dni)<input type="number" min={0} max={3650} value={draft.review_interval_days} onChange={(e) => setDraft({ ...draft, review_interval_days: Number(e.target.value) })} /></label></div><label>Opis<textarea rows={4} maxLength={10000} value={draft.description} onChange={(e) => setDraft({ ...draft, description: e.target.value })} /></label>{error ? <p className="inline-error" role="alert">{error}</p> : null}{notice ? <p className="inline-success" role="status">{notice}</p> : null}<button className="button button--primary" disabled={busy}>{busy ? "Zapisywanie…" : "Zapisz ustawienia"}</button></form>;
}

function PageDetails({ pageID, space, pages, canWrite, changed }: { pageID: string; space: Space; pages: PageSummary[]; canWrite: boolean; changed: () => void }) {
  const detail = useIncidentQuery<KnowledgePageRecord>(`${base}/pages/${pageID}`);
  const versions = useIncidentQuery<{ versions: PageVersion[] }>(`${base}/pages/${pageID}/versions?limit=20`);
  const [editing, setEditing] = useState(false);
  if (detail.loading && !detail.data) return <PageState title="Ładowanie strony" />;
  if (detail.error || !detail.data) return <PageState title="Nie udało się otworzyć strony" detail={detail.error} tone="error" />;
  if (editing) return <PageEditor space={space} pages={pages} record={detail.data} cancel={() => setEditing(false)} saved={() => { setEditing(false); detail.reload(); versions.reload(); changed(); }} />;
  const page = detail.data;
  return <div className="knowledge-document"><header className="knowledge-document-heading"><div><div className="action-row"><span className={page.status === "PUBLISHED" ? "badge badge--success" : "badge"}>{pageStatus[page.status]}</span>{page.review_due_at ? <span className="muted">Przegląd: {new Date(page.review_due_at).toLocaleDateString("pl-PL")}</span> : null}</div><h2>{page.title}</h2><p className="muted">Wersja {page.version} · ostatnia zmiana {new Date(page.updated_at).toLocaleString("pl-PL")} przez {page.updated_by_name || `#${page.updated_by}`}</p></div>{canWrite ? <button className="button button--secondary" onClick={() => setEditing(true)}>Edytuj</button> : null}</header><SafeMarkdown source={page.body} /><aside className="knowledge-history"><h3>Historia wersji</h3>{versions.loading ? <p className="muted">Ładowanie historii…</p> : versions.error ? <p className="inline-error">{versions.error}</p> : <ol>{versions.data?.versions.map((version) => <li key={version.version}><strong>Wersja {version.version}</strong><span>{pageStatus[version.status]} · {new Date(version.changed_at).toLocaleString("pl-PL")} · {version.changed_by_name || `#${version.changed_by}`}</span></li>)}</ol>}</aside></div>;
}

function PageEditor({ space, pages, record, saved, cancel }: { space: Space; pages: PageSummary[]; record?: KnowledgePageRecord; saved: (record: KnowledgePageRecord) => void; cancel?: () => void }) {
  const [params] = useSearchParams();
  const template = knowledgeTemplates.find((item) => item.id === params.get("template"));
  const [draft, setDraft] = useState<{ parent_id: string; title: string; body: string; status: KnowledgePageRecord["status"]; review_due_at: string }>({ parent_id: record?.parent_id ?? "", title: record?.title ?? template?.title ?? "", body: record?.body ?? template?.body ?? "", status: record?.status ?? "DRAFT", review_due_at: dateTime(record?.review_due_at) });
  const [dirty, setDirty] = useState(false); const [busy, setBusy] = useState(false); const [error, setError] = useState<string>(); const [conflict, setConflict] = useState(false);
  const update = (next: Partial<typeof draft>) => { setDraft((value) => ({ ...value, ...next })); setDirty(true); setConflict(false); };
  async function submit(event: FormEvent) {
    event.preventDefault(); setBusy(true); setError(undefined); setConflict(false);
    const body = { parent_id: draft.parent_id || undefined, title: draft.title, body: draft.body, status: draft.status, review_due_at: isoDate(draft.review_due_at), ...(record ? { version: record.version } : {}) };
    try {
      const result = await api<KnowledgePageRecord>(record ? `${base}/pages/${record.id}` : `${base}/spaces/${space.id}/pages`, { method: record ? "PUT" : "POST", headers: record ? undefined : { "Idempotency-Key": crypto.randomUUID() }, body: jsonBody(body) });
      flushSync(() => setDirty(false));
      saved(result);
    } catch (cause) { if (cause instanceof ApiError && cause.status === 409) setConflict(true); setError(errorMessage(cause, "Nie udało się zapisać strony.")); }
    finally { setBusy(false); }
  }
  async function loadLatest() {
    if (!record) return; setBusy(true); setError(undefined);
    try { const latest = await api<KnowledgePageRecord>(`${base}/pages/${record.id}`); setDraft({ parent_id: latest.parent_id ?? "", title: latest.title, body: latest.body, status: latest.status, review_due_at: dateTime(latest.review_due_at) }); setDirty(false); setConflict(false); }
    catch (cause) { setError(errorMessage(cause, "Nie udało się pobrać aktualnej wersji.")); }
    finally { setBusy(false); }
  }
  return <form className="knowledge-editor form-stack" onSubmit={(event) => void submit(event)}><UnsavedGuard dirty={dirty} /><header className="panel-heading"><div><p className="eyebrow">{record ? `Edycja wersji ${record.version}` : template ? template.name : "Nowa strona"}</p><h2>{record ? record.title : "Utwórz stronę"}</h2></div><div className="action-row">{cancel ? <button type="button" className="button button--secondary" onClick={cancel}>Anuluj</button> : <Link className="button button--secondary" to={`/knowledge/${space.id}`}>Anuluj</Link>}<button className="button button--primary" disabled={busy}>{busy ? "Zapisywanie…" : "Zapisz stronę"}</button></div></header>{error ? <p className="inline-error" role="alert">{error}</p> : null}{conflict ? <div className="notice notice--error"><strong>Ktoś zapisał nowszą wersję.</strong><p>Skopiuj potrzebne zmiany, a następnie wczytaj aktualną wersję przed ponowną edycją.</p><button type="button" className="button button--secondary" disabled={busy} onClick={() => void loadLatest()}>Wczytaj aktualną wersję</button></div> : null}<div className="knowledge-editor-meta"><label>Tytuł<input required maxLength={200} value={draft.title} onChange={(e) => update({ title: e.target.value })} /></label><label>Strona nadrzędna<select value={draft.parent_id} onChange={(e) => update({ parent_id: e.target.value })}><option value="">Brak — poziom główny</option>{pages.filter((page) => page.id !== record?.id).map((page) => <option key={page.id} value={page.id}>{page.title}</option>)}</select></label><label>Stan<select value={draft.status} onChange={(e) => update({ status: e.target.value as KnowledgePageRecord["status"] })}><option value="DRAFT">Szkic</option><option value="PUBLISHED">Opublikowana</option></select></label>{draft.status === "PUBLISHED" ? <label>Termin przeglądu<input type="datetime-local" value={draft.review_due_at} onChange={(e) => update({ review_due_at: e.target.value })} /></label> : null}</div><div className="knowledge-editor-columns"><label>Treść w Markdown<textarea required rows={24} maxLength={262144} value={draft.body} onChange={(e) => update({ body: e.target.value })} spellCheck /></label><div><span className="knowledge-preview-label">Podgląd</span><SafeMarkdown source={draft.body} /></div></div></form>;
}
