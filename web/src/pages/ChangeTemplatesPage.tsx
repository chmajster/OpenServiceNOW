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
import { useRemotePage } from "../components/useRemotePage";
import { PageState } from "../components/PageState";
import { PageFeedback, More } from "../cmdb/components";
import { useEditor } from "../requests/useEditor";
import { useCreation } from "../requests/useCreation";
import { ChangeForm } from "../changes/ChangeForm";
import { CISet } from "../changes/CISet";
import { ConfigurationFeedback } from "../changes/ConfigurationFeedback";
import {
  blank,
  labels,
  type Template,
  type TemplateConfig,
  type TemplateSummary,
} from "../changes/contracts";
const empty: Template = {
  id: "0",
  version: "0",
  name: "",
  author_id: "0",
  published_version: null,
  withdrawn: false,
  draft: {
    fields: { ...blank, change_type: "STANDARD" },
    ci_ids: [],
    max_risk: "MEDIUM",
    max_duration_seconds: 3600,
  },
};
export function ChangeTemplatesPage() {
  const { templateID } = useParams(),
    { hasPermission } = useAuth();
  const p = useIncidentQuery<Template>(
    templateID && templateID !== "new"
      ? `/api/v1/changes/templates/${templateID}`
      : null,
  );
  if (
    !["change.template_author", "change.template_approve"].some(hasPermission)
  )
    return <PageState title="Brak uprawnień do konfiguracji szablonów" />;
  if (!templateID) return <TemplateList />;
  if (templateID === "new")
    return hasPermission("change.template_author") ? (
      <TemplateEditor initial={empty} />
    ) : (
      <PageState title="Brak uprawnień do tworzenia szablonów" />
    );
  if (!p.data)
    return (
      <>
        <PageFeedback loading={p.loading} error={p.error} empty={false} />
        <button onClick={p.reload}>Ponów odczyt szablonu</button>
      </>
    );
  return <TemplateEditor key={templateID} initial={p.data} />;
}
function TemplateList() {
  const [params, setParams] = useSearchParams(),
    [q, setQ] = useState(params.get("q") ?? ""),
    { hasPermission } = useAuth();
  const p = useRemotePage<TemplateSummary>(
    `/api/v1/changes/templates?limit=25&q=${encodeURIComponent(params.get("q") ?? "")}`,
    "items",
    "before_id",
  );
  return (
    <>
      <header className="page-heading">
        <h1>Szablony zmian Standard</h1>
        {hasPermission("change.template_author") && (
          <Link className="button button--primary" to="/change-templates/new">
            Nowy szablon
          </Link>
        )}
      </header>
      <p>
        Zatwierdzona wersja określa plany, zakres, zestaw CI, ryzyko, czas i
        proces. Każda instancja wymaga własnej akceptacji.
      </p>
      <form
        className="filter-grid"
        onSubmit={(e) => {
          e.preventDefault();
          setParams(q ? { q } : {});
        }}
      >
        <label>
          Nazwa szablonu
          <input
            value={q}
            maxLength={160}
            onChange={(e) => setQ(e.target.value)}
          />
        </label>
        <button>Wyszukaj szablon</button>
        <button type="button" onClick={p.reload}>
          Odśwież szablony
        </button>
      </form>
      <PageFeedback
        loading={p.loading}
        error={p.error}
        empty={!p.items.length}
      />
      <div className="table-wrap">
        <table>
          <thead>
            <tr>
              <th>Nazwa</th>
              <th>Wersja robocza</th>
              <th>Zatwierdzona wersja</th>
              <th>Dostępność</th>
            </tr>
          </thead>
          <tbody>
            {p.items.map((t) => (
              <tr key={t.id}>
                <td>
                  <Link to={`/change-templates/${t.id}`}>{t.name}</Link>
                </td>
                <td>{t.version}</td>
                <td>{t.published_version ?? "Nie zatwierdzono"}</td>
                <td>{t.withdrawn ? "Wycofany" : "Aktywny"}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <More loading={p.loading} next={p.next} load={p.more} />
    </>
  );
}
function TemplateEditor({ initial }: { initial: Template }) {
  const path = `/api/v1/changes/templates/${initial.id}`,
    ed = useEditor(initial, path),
    creation = useCreation<Template>(),
    navigate = useNavigate(),
    { hasPermission, session } = useAuth();
  const [name, setName] = useState(initial.name),
    [config, setConfig] = useState<TemplateConfig>(initial.draft),
    [version, setVersion] = useState(initial.version),
    [saved, setSaved] = useState(""),
    keep = useRef(false);
  const existing = initial.id !== "0",
    dirty =
      name !== ed.record.name ||
      JSON.stringify(config) !== JSON.stringify(ed.record.draft),
    busy = ed.busy || creation.busy,
    uncertain = ed.uncertain || creation.uncertain,
    blocked = busy || uncertain || !!ed.conflict;
  useEffect(() => {
    setVersion(ed.record.version);
    if (keep.current) {
      keep.current = false;
      return;
    }
    setName(ed.record.name);
    setConfig(ed.record.draft);
  }, [ed.record]);
  useEffect(() => {
    if (saved) navigate(saved, { replace: true });
  }, [saved, navigate]);
  function resolve(replace: boolean) {
    const current = ed.conflict!;
    setVersion(current.version);
    if (!replace) {
      keep.current = true;
    }
    ed.resolve();
  }
  return (
    <>
      <UnsavedGuard dirty={!saved && (dirty || uncertain)} includeSearch />
      <header className="page-heading">
        <div>
          <p className="eyebrow">Standard Change · wersja {version}</p>
          <h1>{existing ? ed.record.name : "Nowy szablon"}</h1>
        </div>
        <Link className="button" to="/change-templates">
          Wróć do szablonów
        </Link>
      </header>
      <p>
        Zatwierdzona wersja: {ed.record.published_version ?? "Brak"} ·{" "}
        {ed.record.withdrawn ? "Wycofany" : "Aktywny"}. Autor ostatniego zapisu
        i zatwierdzający muszą być różnymi osobami.
      </p>
      <ConfigurationFeedback
        editor={ed}
        proposal={{ name, config }}
        current={
          ed.conflict
            ? { name: ed.conflict.name, config: ed.conflict.draft }
            : undefined
        }
        resolve={resolve}
      />
      {creation.error && <p role="alert">{creation.error}</p>}
      <form
        onSubmit={async (e) => {
          e.preventDefault();
          const body = { version, name, config };
          if (existing)
            await ed.save(path, body, "PUT", {
              "Idempotency-Key": crypto.randomUUID(),
            });
          else {
            const result = await creation.create(
              "/api/v1/changes/templates",
              body,
            );
            if (result) setSaved(`/change-templates/${result.id}`);
          }
        }}
      >
        <fieldset
          disabled={blocked || !hasPermission("change.template_author")}
        >
          <label>
            Nazwa szablonu
            <input
              required
              maxLength={160}
              value={name}
              onChange={(e) => setName(e.target.value)}
            />
          </label>
          <ChangeForm
            draft={config.fields}
            change={(fields) => setConfig({ ...config, fields })}
            disabled={blocked}
            templateMode
          />
          <div className="form-grid">
            <label>
              Maksymalne ryzyko
              <select
                value={config.max_risk}
                onChange={(e) =>
                  setConfig({ ...config, max_risk: e.target.value })
                }
              >
                {["LOW", "MEDIUM", "HIGH"].map((v) => (
                  <option key={v} value={v}>
                    {labels[v]}
                  </option>
                ))}
              </select>
            </label>
            <label>
              Maksymalny czas w sekundach
              <input
                type="number"
                min={1}
                max={2592000}
                required
                value={config.max_duration_seconds}
                onChange={(e) =>
                  setConfig({
                    ...config,
                    max_duration_seconds: Number(e.target.value),
                  })
                }
              />
            </label>
          </div>
          <CISet
            ids={config.ci_ids}
            change={(ci_ids) => setConfig({ ...config, ci_ids })}
            disabled={blocked}
          />
        </fieldset>
        {hasPermission("change.template_author") && (
          <button
            className="button button--primary"
            disabled={
              busy ||
              !!ed.conflict ||
              (!uncertain &&
                (!name.trim() || !config.ci_ids.length || (existing && !dirty)))
            }
          >
            {creation.uncertain
              ? "Potwierdź zapis szablonu"
              : "Zapisz wersję roboczą"}
          </button>
        )}
      </form>
      {creation.uncertain && (
        <p role="status">
          Ponowienie zachowuje ostatni szablon i klucz zapisu.
        </p>
      )}
      {existing && (
        <div className="action-row">
          <button
            disabled={blocked || dirty}
            onClick={() => void ed.save(path, undefined, "GET")}
          >
            Odśwież szablon
          </button>
          {hasPermission("change.template_approve") && (
            <>
              <button
                disabled={
                  blocked || dirty || ed.record.author_id === session?.user.id
                }
                onClick={() =>
                  void ed.save(
                    `${path}/publish`,
                    { version, withdraw: false },
                    "POST",
                    { "Idempotency-Key": crypto.randomUUID() },
                  )
                }
              >
                Zatwierdź tę wersję szablonu
              </button>
              <button
                disabled={
                  blocked ||
                  dirty ||
                  ed.record.withdrawn ||
                  !ed.record.published_version
                }
                onClick={() =>
                  void ed.save(
                    `${path}/publish`,
                    { version, withdraw: true },
                    "POST",
                    { "Idempotency-Key": crypto.randomUUID() },
                  )
                }
              >
                Wycofaj szablon z użycia
              </button>
            </>
          )}
        </div>
      )}
    </>
  );
}
