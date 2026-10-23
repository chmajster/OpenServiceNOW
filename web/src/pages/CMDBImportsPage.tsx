import { useEffect, useRef, useState, type ChangeEvent, type FormEvent } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { ApiError, api, jsonBody } from "../api/client";
import { useAuth } from "../auth/AuthContext";
import { PageState } from "../components/PageState";
import { useRemotePage } from "../components/useRemotePage";
import { useIncidentQuery, useIncidentTitleFocus } from "../incidents/hooks";
import { More, PageFeedback } from "../cmdb/components";
import { cmdbLabel, cmdbText, useCMDBLocale } from "../cmdb/messages";
import type { Import, ImportConfig, ImportRow, Metadata } from "../cmdb/contracts";

const terminalStates = new Set(["COMPLETED", "PARTIAL", "FAILED", "CANCELLED"]);
const workingStates = new Set(["QUEUED_PREVIEW", "QUEUED_APPLY", "APPLYING"]);
const t = (key: string, locale: "pl" | "en") => cmdbText(key, locale);

export function parseCSVHeader(source: string): string[] {
  const input = source.replace(/^\uFEFF/, "");
  const values: string[] = [];
  let value = "";
  let quoted = false;
  let closedQuote = false;
  let endedHeader = false;
  for (let index = 0; index < input.length; index += 1) {
    const char = input[index]!;
    if (quoted) {
      if (char === '"') {
        if (input[index + 1] === '"') {
          value += '"';
          index += 1;
        } else {
          quoted = false;
          closedQuote = true;
        }
      } else {
        value += char;
      }
      continue;
    }
    if (char === "," || char === "\r" || char === "\n") {
      values.push(value);
      if (char !== ",") {
        if (char === "\r" && input[index + 1] === "\n") index += 1;
        endedHeader = true;
        break;
      }
      value = "";
      closedQuote = false;
      continue;
    }
    if (char === '"' && value.length === 0 && !closedQuote) {
      quoted = true;
      continue;
    }
    if (closedQuote || char === '"') throw new Error(cmdbText("csvInvalidHeaderQuote"));
    value += char;
  }
  if (quoted) throw new Error(cmdbText("csvUnclosedQuote"));
  if (!endedHeader) values.push(value);
  if (values.some((item) => item.length === 0 || item.length > 160)) throw new Error(cmdbText("csvEmptyOrLongHeader"));
  if (new Set(values).size !== values.length) throw new Error(cmdbText("csvDuplicateHeader"));
  return values;
}

export function CMDBImportsPage() {
  const locale = useCMDBLocale();
  const heading = useIncidentTitleFocus();
  const navigate = useNavigate();
  const { hasPermission } = useAuth();
  const schema = useIncidentQuery<Metadata>("/api/v1/cmdb/classes");
  const page = useRemotePage<Import>("/api/v1/cmdb/imports?limit=25", "items", "before_id");
  const canImportCI = hasPermission("cmdb.import") && hasPermission("cmdb.manage") && hasPermission("cmdb.read");
  const canImportAsset = hasPermission("cmdb.import") && hasPermission("assets.manage") && hasPermission("assets.read");

  if (!canImportCI && !canImportAsset) return <PageState title={locale === "en" ? "Import permission required" : "Brak uprawnień do importu"} tone="error" />;

  return (
    <section className="page">
      <div className="page-heading">
        <div><p className="eyebrow"><Link to="/cmdb">{t("title", locale)}</Link></p><h1 ref={heading} tabIndex={-1}>{t("importTitle", locale)}</h1><p className="muted">{t("importSubtitle", locale)}</p></div>
      </div>
      {schema.loading && <PageState title={t("loading", locale)} />}
      {schema.error && <><PageState title={t("loadError", locale)} detail={schema.error} tone="error" /><button className="button button--secondary mt-3" onClick={schema.reload}>{t("retry", locale)}</button></>}
      {schema.data && <ImportCreateForm
        metadata={schema.data}
        canImportCI={canImportCI}
        canImportAsset={canImportAsset}
        onCreated={(record) => navigate("/cmdb/imports/" + record.id)}
      />}
      <section className="panel mt-5">
        <h2 className="text-lg font-semibold">{t("imports", locale)}</h2>
        <PageFeedback loading={page.loading} error={page.error} empty={!page.items.length} />
        {page.error && <button className="button button--secondary mt-3" onClick={page.retry}>{t("retry", locale)}</button>}
        {page.items.length > 0 && <div className="table-wrap mt-3"><table>
          <thead><tr><th>{t("file", locale)}</th><th>{t("target", locale)}</th><th>{t("rowState", locale)}</th><th>{t("counts", locale)}</th></tr></thead>
          <tbody>{page.items.map((record) => <tr key={record.id}>
            <td><Link to={"/cmdb/imports/" + record.id}>{record.file_name}</Link><small className="block text-slate-500">{new Date(record.created_at).toLocaleString(locale === "en" ? "en-GB" : "pl-PL")}</small></td>
            <td>{record.target === "CI" ? t("targetCI", locale) : t("targetAsset", locale)}</td>
            <td>{cmdbLabel(record.state, locale)}{record.job_status ? " · " + cmdbLabel(record.job_status, locale) : ""}</td>
            <td>{Object.entries(record.counts ?? {}).map(([state, count]) => cmdbLabel(state, locale) + ": " + count).join(" · ") || "—"}</td>
          </tr>)}</tbody>
        </table></div>}
        <More loading={page.loading} next={page.next} load={() => void page.more()} />
      </section>
    </section>
  );
}

function ImportCreateForm({
  metadata,
  canImportCI,
  canImportAsset,
  onCreated,
}: {
  metadata: Metadata;
  canImportCI: boolean;
  canImportAsset: boolean;
  onCreated: (record: Import) => void;
}) {
  const locale = useCMDBLocale();
  const initialTarget: "CI" | "ASSET" = canImportCI ? "CI" : "ASSET";
  const [target, setTarget] = useState<"CI" | "ASSET">(initialTarget);
  const identityOptions = target === "CI" ? metadata.ci_import_identity_keys : metadata.asset_import_identity_keys;
  const fields = target === "CI" ? metadata.ci_import_fields : metadata.asset_import_fields;
  const requiredFields = target === "CI" ? metadata.ci_import_required_fields : metadata.asset_import_required_fields;
  const [identity, setIdentity] = useState(identityOptions[0] ?? "");
  const [file, setFile] = useState<File | null>(null);
  const [headers, setHeaders] = useState<string[]>([]);
  const [mapping, setMapping] = useState<Record<string, string>>({});
  const [fileError, setFileError] = useState<string>();
  const [error, setError] = useState<string>();
  const [busy, setBusy] = useState(false);
  const pending = useRef(false);
  const operation = useRef<{ signature: string; key: string } | null>(null);

  function changeTarget(value: "CI" | "ASSET") {
    setTarget(value);
    const choices = value === "CI" ? metadata.ci_import_identity_keys : metadata.asset_import_identity_keys;
    setIdentity(choices[0] ?? "");
    setMapping({});
  }

  async function selectFile(event: ChangeEvent<HTMLInputElement>) {
    const selected = event.target.files?.[0] ?? null;
    setFile(selected);
    setHeaders([]);
    setMapping({});
    setFileError(undefined);
    setError(undefined);
    if (!selected) return;
    if (selected.size > metadata.max_csv_bytes) {
      setFileError(locale === "en" ? "The file exceeds the server limit." : "Plik przekracza limit serwera.");
      return;
    }
    try {
      const bytes = await selected.slice(0, 65536).arrayBuffer();
      const prefix = new TextDecoder("utf-8", { fatal: true }).decode(bytes);
      const columns = parseCSVHeader(prefix);
      if (columns.length > metadata.max_csv_columns) throw new Error(locale === "en" ? "Too many columns." : "Za dużo kolumn.");
      setHeaders(columns);
      setMapping(Object.fromEntries(fields.filter((key) => columns.includes(key)).map((key) => [key, key])));
    } catch (cause) {
      setFileError(cause instanceof Error ? cause.message : String(cause));
    }
  }

  const configured = requiredFields.every((field) => mapping[field]) && Boolean(mapping[identity]);
  const mappingsUnique = new Set(Object.values(mapping).filter(Boolean)).size === Object.values(mapping).filter(Boolean).length;

  async function submit(event: FormEvent) {
    event.preventDefault();
    if (pending.current || !file || fileError || !headers.length || !configured || !mappingsUnique) return;
    pending.current = true;
    setBusy(true);
    setError(undefined);
    const config: ImportConfig = { target, identity_key: identity, mapping: Object.fromEntries(Object.entries(mapping).filter(([, value]) => value)) };
    const signature = target + ":" + identity + ":" + file.name + ":" + file.size + ":" + JSON.stringify(config.mapping);
    if (!operation.current || operation.current.signature !== signature) operation.current = { signature, key: crypto.randomUUID() };
    const body = new FormData();
    body.set("file", file);
    body.set("config", JSON.stringify(config));
    try {
      const created = await api<Import>("/api/v1/cmdb/imports", {
        method: "POST",
        body,
        headers: { "Idempotency-Key": operation.current.key },
      });
      onCreated(created);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      pending.current = false;
      setBusy(false);
    }
  }

  const visibleFields = [...fields].sort((left, right) => Number(requiredFields.includes(right)) - Number(requiredFields.includes(left)) || left.localeCompare(right));

  return (
    <form className="panel form-grid" onSubmit={submit}>
      <h2 className="text-lg font-semibold">{t("preview", locale)}</h2>
      {!canImportCI && target === "CI" && <PageState title={locale === "en" ? "CI import requires cmdb.manage, cmdb.read and cmdb.import." : "Import CI wymaga uprawnień cmdb.manage, cmdb.read i cmdb.import."} tone="error" />}
      {!canImportAsset && target === "ASSET" && <PageState title={locale === "en" ? "Asset import requires assets.manage, assets.read and cmdb.import." : "Import Asset wymaga uprawnień assets.manage, assets.read i cmdb.import."} tone="error" />}
      <label>{t("target", locale)}<select value={target} disabled={busy} onChange={(event) => changeTarget(event.target.value as "CI" | "ASSET")}>
        {canImportCI && <option value="CI">{t("targetCI", locale)}</option>}
        {canImportAsset && <option value="ASSET">{t("targetAsset", locale)}</option>}
      </select></label>
      <label>{t("identity", locale)}<select value={identity} disabled={busy} onChange={(event) => setIdentity(event.target.value)}>{identityOptions.map((key) => <option key={key} value={key}>{key}</option>)}</select></label>
      <label>{t("file", locale)}<input type="file" accept=".csv,text/csv" disabled={busy} onChange={(event) => void selectFile(event)} /><small>{locale === "en" ? "Maximum " : "Maksymalnie "}{metadata.max_csv_bytes} {locale === "en" ? "bytes, " : "bajtów, "}{metadata.max_csv_rows} {locale === "en" ? "rows and " : "wierszy i "}{metadata.max_csv_columns} {locale === "en" ? "columns." : "kolumn."}</small></label>
      {file && <p className="muted">{file.name} · {file.size} B · {headers.length ? headers.join(", ") : ""}</p>}
      {fileError && <PageState title={locale === "en" ? "CSV header could not be read" : "Nie można odczytać nagłówka CSV"} detail={fileError} tone="error" />}
      {headers.length > 0 && <fieldset className="form-grid border-t border-slate-200 pt-4">
        <legend className="font-semibold">{t("mapping", locale)}</legend>
        {visibleFields.map((field) => {
          const usedByOther = new Set(Object.entries(mapping).filter(([key, value]) => key !== field && value).map(([, value]) => value));
          const isRequired = requiredFields.includes(field);
          return <label key={field}>
            {field}{isRequired ? " *" : ""}
            <select value={mapping[field] ?? ""} disabled={busy} onChange={(event) => setMapping((current) => ({ ...current, [field]: event.target.value }))}>
              <option value="">{t("unmapped", locale)}</option>
              {headers.map((header) => <option key={header} value={header} disabled={usedByOther.has(header)}>{header}</option>)}
            </select>
            {isRequired && !mapping[field] && <small className="text-rose-700">{t("requiredMapping", locale)}</small>}
          </label>;
        })}
        {mapping.owner_id && <p className="muted">{t("ownerImportHint", locale)}</p>}
      </fieldset>}
      {error && <PageState title={locale === "en" ? "Import was not created" : "Nie utworzono importu"} detail={error} tone="error" />}
      <div className="action-row"><button className="button button--primary" disabled={busy || !file || !headers.length || Boolean(fileError) || !configured || !mappingsUnique || target === "CI" && !canImportCI || target === "ASSET" && !canImportAsset}>{busy ? t("previewCreating", locale) : t("startPreview", locale)}</button></div>
    </form>
  );
}

export function CMDBImportDetailPage() {
  const locale = useCMDBLocale();
  const { importID } = useParams();
  const heading = useIncidentTitleFocus(importID);
  const { hasPermission } = useAuth();
  const detail = useIncidentQuery<Import>(importID ? "/api/v1/cmdb/imports/" + importID : null);
  const rows = useRemotePage<ImportRow>(importID ? "/api/v1/cmdb/imports/" + importID + "/rows?limit=100" : "/api/v1/cmdb/imports/0/rows?limit=100", "items", "after_row", Boolean(importID));
  const [error, setError] = useState<string>();
  const [busy, setBusy] = useState(false);
  const [confirmCancel, setConfirmCancel] = useState(false);
  const pending = useRef(false);
  const detailReload = useRef(detail.reload);
  const rowsReload = useRef(rows.reload);
  detailReload.current = detail.reload;
  rowsReload.current = rows.reload;
  const record = detail.data;
  const canImportCI = hasPermission("cmdb.import") && hasPermission("cmdb.manage") && hasPermission("cmdb.read");
  const canImportAsset = hasPermission("cmdb.import") && hasPermission("assets.manage") && hasPermission("assets.read");
  const allowed = record?.target === "CI" ? canImportCI : canImportAsset;
  const invalidRows = (record?.counts.ERROR ?? 0) + (record?.counts.CONFLICT ?? 0);

  useEffect(() => {
    if (!importID || !record || !workingStates.has(record.state)) return;
    const timer = window.setInterval(() => {
      detailReload.current();
      rowsReload.current();
    }, 2500);
    return () => window.clearInterval(timer);
  }, [importID, record?.state]);

  async function action(name: "apply" | "review" | "cancel") {
    if (!record || pending.current) return;
    pending.current = true;
    setBusy(true);
    setError(undefined);
    try {
      await api<Import>("/api/v1/cmdb/imports/" + record.id + "/" + name, {
        method: "POST",
        body: jsonBody({ version: record.version }),
      });
      setConfirmCancel(false);
      detail.reload();
      rows.reload();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
      if (cause instanceof ApiError && cause.status === 409) {
        detail.reload();
        rows.reload();
      }
    } finally {
      pending.current = false;
      setBusy(false);
    }
  }

  if (detail.loading) return <PageState title={t("loading", locale)} />;
  if (detail.error || !record) return <section className="page"><PageState title={t("loadError", locale)} detail={detail.error} tone="error" />{detail.error && <button className="button button--secondary mt-3" onClick={detail.reload}>{t("retry", locale)}</button>}</section>;

  return (
    <section className="page">
      <div className="page-heading">
        <div><p className="eyebrow"><Link to="/cmdb/imports">{t("backToList", locale)} · {t("imports", locale)}</Link></p><h1 ref={heading} tabIndex={-1}>{record.file_name}</h1><p className="muted">{record.target === "CI" ? t("targetCI", locale) : t("targetAsset", locale)} · {cmdbLabel(record.state, locale)}</p></div>
        {allowed && <div className="action-row">
          {record.state === "PREVIEW_READY" && <button className="button button--primary" disabled={busy || invalidRows > 0} onClick={() => void action("apply")}>{t("applyImport", locale)}</button>}
          {["PREVIEW_READY", "PARTIAL", "FAILED"].includes(record.state) && <button className="button button--secondary" disabled={busy} onClick={() => void action("review")}>{t("reviewImport", locale)}</button>}
          {!terminalStates.has(record.state) && !confirmCancel && <button className="button button--secondary" disabled={busy} onClick={() => setConfirmCancel(true)}>{t("cancelImport", locale)}</button>}
          {!terminalStates.has(record.state) && confirmCancel && <><span role="status">{locale === "en" ? "Cancel this import?" : "Anulować ten import?"}</span><button className="button button--danger" disabled={busy} onClick={() => void action("cancel")}>{locale === "en" ? "Confirm cancellation" : "Potwierdź anulowanie"}</button><button className="button button--secondary" disabled={busy} onClick={() => setConfirmCancel(false)}>{t("cancel", locale)}</button></>}
        </div>}
      </div>
      {error && <PageState title={locale === "en" ? "Import action failed" : "Nie udało się wykonać akcji importu"} detail={error} tone="error" />}
      {!allowed && <PageState title={locale === "en" ? "Import access is no longer available." : "Uprawnienia do tego importu zostały odebrane."} tone="error" />}
      <section className="panel">
        <h2 className="text-lg font-semibold">{t("counts", locale)}</h2>
        <div className="mt-3 grid gap-2 sm:grid-cols-2 lg:grid-cols-4">
          <p>{t("rowState", locale)}: <strong>{cmdbLabel(record.state, locale)}</strong></p>
          <p>{t("job", locale)}: <strong>{record.job_status ? cmdbLabel(record.job_status, locale) : "—"}</strong></p>
          <p>{locale === "en" ? "Attempts" : "Próby"}: <strong>{record.attempts}</strong></p>
          <p>{locale === "en" ? "Progress" : "Postęp"}: <strong>{record.progress}%</strong></p>
        </div>
        <progress className="mt-2 w-full" max="100" value={record.progress} aria-label={locale === "en" ? "Import progress" : "Postęp importu"} />
        <dl className="mt-4 grid gap-2 sm:grid-cols-2">
          {Object.entries(record.counts ?? {}).map(([state, count]) => <div key={state}><dt className="text-sm text-slate-500">{cmdbLabel(state, locale)}</dt><dd className="font-medium">{count}</dd></div>)}
        </dl>
      </section>
      {record.state === "PREVIEW_READY" && <PageState title={t("importWarning", locale)} />}
      {invalidRows > 0 && <PageState title={t("importHasErrors", locale)} tone="error" />}
      {record.error_code && <PageState title={locale === "en" ? "Worker error" : "Błąd workera"} detail={record.error_code} tone="error" />}
      <section className="panel mt-5">
        <h2 className="text-lg font-semibold">{t("preview", locale)}</h2>
        <PageFeedback loading={rows.loading} error={rows.error} empty={!rows.items.length} />
        {rows.error && <button className="button button--secondary mt-3" onClick={rows.retry}>{t("retry", locale)}</button>}
        {rows.items.length > 0 && <div className="table-wrap mt-3"><table>
          <thead><tr><th>{t("row", locale)}</th><th>{t("identityValue", locale)}</th><th>{t("action", locale)}</th><th>{t("rowState", locale)}</th><th>{t("error", locale)}</th><th>{t("rowDetails", locale)}</th></tr></thead>
          <tbody>{rows.items.map((row) => <tr key={row.row_number}>
            <td>{row.row_number}</td><td>{row.identity_value}</td><td>{cmdbLabel(row.action, locale)}</td><td>{cmdbLabel(row.state, locale)}</td><td>{row.error_message || row.error_code || "—"}</td>
            <td><details><summary>{t("rowDetails", locale)}</summary><p>{t("before", locale)}</p><pre className="max-w-xl overflow-auto text-xs">{JSON.stringify(row.before, null, 2)}</pre><p>{t("after", locale)}</p><pre className="max-w-xl overflow-auto text-xs">{JSON.stringify(row.after, null, 2)}</pre></details></td>
          </tr>)}</tbody>
        </table></div>}
        <More loading={rows.loading} next={rows.next} load={() => void rows.more()} />
      </section>
    </section>
  );
}
