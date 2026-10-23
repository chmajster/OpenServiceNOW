import { useEffect, useRef, useState, type FormEvent } from "react";
import { Link, useNavigate, useParams, useSearchParams } from "react-router-dom";
import { api, ApiError, jsonBody } from "../api/client";
import { useAuth } from "../auth/AuthContext";
import { More, PageFeedback } from "../cmdb/components";
import { labels, text, type Asset, type CI } from "../cmdb/contracts";
import { InventoryCatalogCombobox } from "../components/InventoryCatalogCombobox";
import { PageState } from "../components/PageState";
import { RemoteCombobox, type Choice } from "../components/RemoteCombobox";
import { useRemotePage } from "../components/useRemotePage";
import { useIncidentQuery, useIncidentTitleFocus } from "../incidents/hooks";

const stateOptions = ["ORDERED", "IN_STOCK", "IN_USE", "IN_REPAIR", "RETIRED", "DISPOSED"];
const transitions: Record<string, string[]> = {
  ORDERED: ["IN_STOCK", "RETIRED"],
  IN_STOCK: ["IN_USE", "IN_REPAIR", "RETIRED"],
  IN_USE: ["IN_STOCK", "IN_REPAIR", "RETIRED"],
  IN_REPAIR: ["IN_STOCK", "IN_USE", "RETIRED"],
  RETIRED: ["DISPOSED"],
  DISPOSED: [],
};
type Draft = {
  asset_tag: string;
  name: string;
  owner: Choice | null;
  serial_number: string;
  vendor: string;
  model: string;
  location: string;
  state: string;
  purchase_date: string;
  purchase_cost: string;
  currency: string;
  warranty_until: string;
};

const emptyDraft = (): Draft => ({
  asset_tag: "",
  name: "",
  owner: null,
  serial_number: "",
  vendor: "",
  model: "",
  location: "",
  state: "IN_STOCK",
  purchase_date: "",
  purchase_cost: "",
  currency: "PLN",
  warranty_until: "",
});

function draftFrom(asset: Asset): Draft {
  return {
    asset_tag: asset.asset_tag,
    name: asset.name,
    owner: { id: asset.owner_id, label: asset.owner_name },
    serial_number: asset.serial_number,
    vendor: asset.vendor,
    model: asset.model,
    location: asset.location,
    state: asset.state,
    purchase_date: asset.purchase_date,
    purchase_cost: asset.purchase_cost,
    currency: asset.currency,
    warranty_until: asset.warranty_until,
  };
}

export function AssetsPage() {
  const { session, hasPermission } = useAuth();
  const { assetID } = useParams();
  const readable = hasPermission("assets.read") || hasPermission("assets.read_own");
  if (!readable)
    return <PageState title="Brak dostępu do zasobów IT" detail="Potrzebujesz uprawnienia do odczytu ewidencji zasobów." tone="error" />;
  return <AssetWorkspace key={`${session?.tenant.id}:${session?.user.id}`} assetID={assetID} />;
}

function AssetWorkspace({ assetID }: { assetID?: string }) {
  if (!assetID) return <AssetList />;
  if (assetID === "new") return <AssetEditor />;
  return <AssetDetails id={assetID} />;
}

function AssetList() {
  const heading = useIncidentTitleFocus();
  const { hasPermission } = useAuth();
  const canManage = hasPermission("assets.manage") && hasPermission("assets.read");
  const [params, setParams] = useSearchParams();
  const query = params.get("q") ?? "";
  const state = params.get("state") ?? "";
  const archived = params.get("archived") ?? "false";
  const [search, setSearch] = useState(query);
  useEffect(() => setSearch(query), [query]);
  const queryParams = new URLSearchParams({ limit: "25", archived });
  if (query) queryParams.set("q", query);
  if (state) queryParams.set("state", state);
  const page = useRemotePage<Asset>(`/api/v1/cmdb/assets?${queryParams}`, "items", "before_id");

  function update(values: Record<string, string>) {
    const next = new URLSearchParams(params);
    next.delete("before_id");
    for (const [key, value] of Object.entries(values)) {
      if (value) next.set(key, value);
      else next.delete(key);
    }
    setParams(next);
  }

  return (
    <section className="page">
      <div className="page-heading">
        <div>
          <p className="eyebrow">IT Asset Management</p>
          <h1 ref={heading} tabIndex={-1}>Zasoby IT</h1>
          <p className="muted">Ewidencja właścicieli, cyklu życia, kosztów, gwarancji i powiązań z konfiguracją.</p>
        </div>
        {canManage && <Link className="button button--primary" to="/assets/new">Nowy zasób</Link>}
      </div>

      <form className="panel grid gap-4 md:grid-cols-[minmax(16rem,1fr)_13rem_13rem_auto] md:items-end" onSubmit={(event) => { event.preventDefault(); update({ q: search.trim() }); }}>
        <label className="min-w-0">
          Szukaj nazwy, tagu lub numeru seryjnego
          <input type="search" value={search} maxLength={160} onChange={(event) => setSearch(event.target.value)} />
        </label>
        <label>
          Cykl życia
          <select aria-label="Cykl życia" value={state} onChange={(event) => update({ state: event.target.value })}>
            <option value="">Wszystkie stany</option>
            {stateOptions.map((value) => <option key={value} value={value}>{labels[value]}</option>)}
          </select>
        </label>
        <label>
          Widok archiwum
          <select aria-label="Widok archiwum" value={archived} onChange={(event) => update({ archived: event.target.value })}>
            <option value="false">Aktywne zasoby</option>
            <option value="true">Archiwum</option>
            <option value="">Wszystkie</option>
          </select>
        </label>
        <button className="button button--secondary" type="submit">Szukaj</button>
      </form>

      <PageFeedback loading={page.loading} error={page.error} empty={!page.items.length} />
      {page.error && <button className="button button--secondary" onClick={page.retry}>Spróbuj ponownie</button>}
      {!page.loading && !page.error && page.items.length > 0 && (
        <div className="panel overflow-hidden p-0">
          <div className="overflow-x-auto">
            <table className="min-w-[900px] w-full">
              <thead><tr><th>Tag zasobu</th><th>Nazwa / model</th><th>Numer seryjny</th><th>Właściciel</th><th>Lokalizacja</th><th>Stan</th><th>Gwarancja do</th></tr></thead>
              <tbody>
                {page.items.map((asset) => (
                  <tr key={asset.id}>
                    <td><Link className="font-semibold text-sn-teal hover:underline" to={`/assets/${asset.id}`}>{asset.asset_tag}</Link>{asset.archived && <span className="ml-2 rounded bg-slate-100 px-2 py-1 text-xs text-slate-600">Archiwum</span>}</td>
                    <td><strong>{asset.name}</strong><small className="block text-slate-500">{[asset.vendor, asset.model].filter(Boolean).join(" ") || "Brak producenta i modelu"}</small></td>
                    <td>{asset.serial_number || "—"}</td>
                    <td>{asset.owner_name}</td>
                    <td>{asset.location || "—"}</td>
                    <td><span className="tag">{labels[asset.state] ?? asset.state}</span></td>
                    <td>{asset.warranty_until || "—"}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <div className="flex justify-end p-4"><More loading={page.loading} next={page.next} load={() => void page.more()} /></div>
        </div>
      )}
    </section>
  );
}

function AssetDetails({ id }: { id: string }) {
  const { hasPermission } = useAuth();
  const query = useIncidentQuery<Asset>(`/api/v1/cmdb/assets/${id}`);
  const [asset, setAsset] = useState<Asset>();
  const [editing, setEditing] = useState(false);
  useEffect(() => { if (query.data) setAsset(query.data); }, [query.data]);
  if (query.loading && !asset) return <PageState title="Ładowanie zasobu…" />;
  if (!asset) return <><PageState title="Nie udało się otworzyć zasobu" detail={query.error} tone="error" /><button className="button button--secondary" onClick={query.reload}>Spróbuj ponownie</button></>;
  const canManage = hasPermission("assets.manage") && hasPermission("assets.read");
  return (
    <section className="page">
      <div className="mb-5 flex flex-wrap items-start justify-between gap-4">
        <div>
          <Link className="text-sm text-sn-teal hover:underline" to="/assets">← Ewidencja zasobów</Link>
          <p className="eyebrow mt-4">IT Asset Management · {asset.asset_tag}</p>
          <h1>{asset.name}</h1>
          <p className="muted">{[asset.vendor, asset.model].filter(Boolean).join(" ") || "Zasób IT"} · {labels[asset.state] ?? asset.state}{asset.archived ? " · W archiwum" : ""}</p>
        </div>
        {canManage && !asset.archived && !editing && <button className="button button--primary" onClick={() => setEditing(true)}>Edytuj zasób</button>}
      </div>

      {editing ? (
        <AssetEditor initial={asset} onCancel={() => setEditing(false)} onSaved={(updated) => { setAsset(updated); setEditing(false); }} />
      ) : (
        <>
          <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
            <SummaryCard label="Tag zasobu" value={asset.asset_tag} />
            <SummaryCard label="Numer seryjny" value={asset.serial_number || "Nie podano"} />
            <SummaryCard label="Właściciel" value={asset.owner_name} />
            <SummaryCard label="Lokalizacja" value={asset.location || "Nie podano"} />
            <SummaryCard label="Zakup" value={asset.purchase_date || "Nie podano"} />
            <SummaryCard label="Koszt" value={asset.purchase_cost ? `${asset.purchase_cost} ${asset.currency}` : "Nie podano"} />
            <SummaryCard label="Gwarancja do" value={asset.warranty_until || "Nie podano"} />
            <SummaryCard label="Ostatnia zmiana" value={new Date(asset.updated_at).toLocaleString("pl-PL")} />
          </div>

          {hasPermission("cmdb.read") ? <LinkedCI asset={asset} /> : null}
          <ArchiveControl asset={asset} onChanged={setAsset} />
          <section className="panel mt-5">
            <div className="mb-4 flex flex-wrap items-center justify-between gap-3">
              <div><h2 className="text-lg font-semibold">Historia zasobu</h2><p className="muted">Zapis audytowy zdarzeń i zmian właściciela lub cyklu życia.</p></div>
              <button className="button button--secondary" disabled={query.loading} onClick={query.reload}>Odśwież dane</button>
            </div>
            <AssetHistory id={asset.id} />
          </section>
        </>
      )}
    </section>
  );
}

function SummaryCard({ label, value }: { label: string; value: string }) {
  return <div className="rounded-lg border border-sn-line bg-white p-4 shadow-sm"><p className="text-sm text-slate-500">{label}</p><p className="mt-1 break-words font-semibold text-slate-900">{value}</p></div>;
}

function AssetEditor({ initial, onCancel, onSaved }: { initial?: Asset; onCancel?: () => void; onSaved?: (asset: Asset) => void }) {
  const navigate = useNavigate();
  const { hasPermission } = useAuth();
  const canManage = hasPermission("assets.manage") && hasPermission("assets.read");
  const [draft, setDraft] = useState(() => initial ? draftFrom(initial) : emptyDraft());
  const [busy, setBusy] = useState(false);
  const pending = useRef(false);
  const createKey = useRef(crypto.randomUUID());
  const [error, setError] = useState<string>();
  if (!canManage) return <PageState title="Brak uprawnień do edycji zasobów" detail="Wymagane są uprawnienia assets.read i assets.manage." tone="error" />;

  function set<K extends keyof Draft>(key: K, value: Draft[K]) {
    setDraft((current) => ({ ...current, [key]: value }));
    setError(undefined);
  }
  async function submit(event: FormEvent) {
    event.preventDefault();
    if (pending.current) return;
    if (!draft.owner) { setError("Wybierz aktywnego właściciela z bieżącej organizacji."); return; }
    if (Array.from(draft.asset_tag.trim()).length < 1 || Array.from(draft.asset_tag.trim()).length > 100) { setError("Tag zasobu musi mieć od 1 do 100 znaków."); return; }
    if (draft.purchase_date && draft.warranty_until && draft.warranty_until < draft.purchase_date) { setError("Data końca gwarancji nie może być wcześniejsza niż data zakupu."); return; }
    if (draft.purchase_cost && !/^(0|[1-9][0-9]{0,9})(\.[0-9]{1,2})?$/.test(draft.purchase_cost)) { setError("Koszt musi być nieujemną kwotą z maksymalnie dwoma miejscami po przecinku."); return; }
    pending.current = true;
    setBusy(true);
    setError(undefined);
    try {
      const fields = {
        asset_tag: draft.asset_tag,
        name: draft.name,
        owner_id: draft.owner.id,
        serial_number: draft.serial_number,
        vendor: draft.vendor,
        model: draft.model,
        location: draft.location,
        state: draft.state,
        purchase_date: draft.purchase_date,
        purchase_cost: draft.purchase_cost,
        currency: draft.currency,
        warranty_until: draft.warranty_until,
      };
      const saved = await api<Asset>(initial ? `/api/v1/cmdb/assets/${initial.id}` : "/api/v1/cmdb/assets", {
        method: initial ? "PUT" : "POST",
        ...(initial ? {} : { headers: { "Idempotency-Key": createKey.current } }),
        body: jsonBody({ ...fields, ...(initial ? { version: initial.version } : {}) }),
      });
      if (onSaved) onSaved(saved);
      else navigate(`/assets/${saved.id}`, { replace: true });
    } catch (cause) {
      setError(cause instanceof ApiError && cause.status === 409
        ? `${cause.message} Odśwież listę lub aktualny rekord przed ponowieniem.`
        : text(cause));
    } finally {
      pending.current = false;
      setBusy(false);
    }
  }
  const allowedStates = initial ? [initial.state, ...(transitions[initial.state] ?? [])] : stateOptions;
  return (
    <form className="panel space-y-5" onSubmit={(event) => void submit(event)}>
      <div><h2 className="text-xl font-semibold">{initial ? "Edycja zasobu" : "Nowy zasób IT"}</h2><p className="muted">Dane zapisywane są w ewidencji organizacji; każda zmiana trafia do audytu.</p><p className="muted text-sm">Producent, model i lokalizacja korzystają ze wspólnych list organizacji. Wybierz zapisaną wartość albo dodaj nową z pola; właściciela wyszukasz po nazwisku lub e-mailu.</p></div>
      {error && <PageState title="Nie zapisano zasobu" detail={error} tone="error" />}
      <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
        <label>Tag zasobu<input required maxLength={100} pattern="[A-Za-z0-9][A-Za-z0-9._:-]{0,99}" title="Użyj liter, cyfr oraz znaków . _ : -; tag musi zaczynać się od litery lub cyfry." value={draft.asset_tag} onChange={(event) => set("asset_tag", event.target.value)} /></label>
        <label>Nazwa<input required maxLength={160} value={draft.name} onChange={(event) => set("name", event.target.value)} /></label>
        <RemoteCombobox kind="users" label="Właściciel" value={draft.owner} onChange={(owner) => set("owner", owner)} endpoint="/api/v1/cmdb/lookups/users" collection="items" cursor="before_id" maxQueryLength={254} disabled={busy} />
        <label>Numer seryjny<input maxLength={128} pattern="[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}" title="Numer seryjny może zawierać litery, cyfry oraz znaki . _ : / - ." value={draft.serial_number} onChange={(event) => set("serial_number", event.target.value)} /></label>
        <InventoryCatalogCombobox kind="manufacturers" value={draft.vendor} onChange={(vendor) => { setDraft((current) => ({ ...current, vendor, model: vendor === current.vendor ? current.model : "" })); setError(undefined); }} disabled={busy} />
        <InventoryCatalogCombobox kind="models" value={draft.model} manufacturer={draft.vendor} onChange={(model) => set("model", model)} disabled={busy} />
        <InventoryCatalogCombobox kind="locations" value={draft.location} onChange={(location) => set("location", location)} disabled={busy} />
        <label>Stan<select value={draft.state} onChange={(event) => set("state", event.target.value)}>{allowedStates.map((value) => <option key={value} value={value}>{labels[value]}</option>)}</select></label>
        <label>Data zakupu<input type="date" value={draft.purchase_date} onChange={(event) => set("purchase_date", event.target.value)} /></label>
        <label>Koszt zakupu<input inputMode="decimal" maxLength={13} placeholder="0,00" value={draft.purchase_cost} onChange={(event) => set("purchase_cost", event.target.value.replace(",", "."))} /></label>
        <label>Waluta<select value={draft.currency} onChange={(event) => set("currency", event.target.value)}>{["PLN", "EUR", "USD", "GBP", "CZK"].map((currency) => <option key={currency}>{currency}</option>)}</select></label>
        <label>Gwarancja do<input type="date" value={draft.warranty_until} onChange={(event) => set("warranty_until", event.target.value)} /></label>
      </div>
      <div className="flex flex-wrap justify-end gap-3">
        {onCancel ? <button className="button button--secondary" type="button" disabled={busy} onClick={onCancel}>Anuluj</button> : <Link className="button button--secondary" to="/assets">Anuluj</Link>}
        <button className="button button--primary" disabled={busy}>{busy ? "Zapisywanie…" : initial ? "Zapisz zmiany" : "Utwórz zasób"}</button>
      </div>
    </form>
  );
}

function ArchiveControl({ asset, onChanged }: { asset: Asset; onChanged: (asset: Asset) => void }) {
  const { hasPermission } = useAuth();
  const [reason, setReason] = useState("");
  const [error, setError] = useState<string>();
  const [busy, setBusy] = useState(false);
  const pending = useRef(false);
  if (!hasPermission("assets.manage") || !hasPermission("assets.read")) return null;
  async function archive(event: FormEvent) {
    event.preventDefault();
    if (pending.current) return;
    const cleanReason = reason.trim();
    if (!cleanReason || Array.from(cleanReason).length > 1000) { setError("Podaj powód o długości od 1 do 1000 znaków."); return; }
    pending.current = true;
    setBusy(true);
    setError(undefined);
    try {
      const updated = await api<Asset>(`/api/v1/cmdb/assets/${asset.id}/archive`, {
        method: "POST",
        body: jsonBody({ version: asset.version, archived: !asset.archived, reason: cleanReason }),
      });
      onChanged(updated);
      setReason("");
    } catch (cause) { setError(text(cause)); }
    finally { pending.current = false; setBusy(false); }
  }
  return (
    <form className="mt-5 rounded-lg border border-amber-200 bg-amber-50 p-4" onSubmit={(event) => void archive(event)}>
      <div className="grid gap-4 md:grid-cols-[1fr_auto] md:items-end">
        <label>Powód {asset.archived ? "przywrócenia" : "archiwizacji"}<input maxLength={1000} required value={reason} onChange={(event) => { setReason(event.target.value); setError(undefined); }} /></label>
        <button className="button button--secondary" disabled={busy}>{busy ? "Zapisywanie…" : asset.archived ? "Przywróć zasób" : "Archiwizuj zasób"}</button>
      </div>
      {error && <p className="mt-3 text-sm text-red-700" role="alert">{error}</p>}
    </form>
  );
}

function LinkedCI({ asset }: { asset: Asset }) {
  const result = useIncidentQuery<{ ci?: CI }>(`/api/v1/cmdb/assets/${asset.id}/ci`, asset.version);
  if (result.loading) return <section className="panel mt-5"><PageState title="Sprawdzanie powiązania z CMDB…" /></section>;
  return (
    <section className="panel mt-5">
      <h2 className="text-lg font-semibold">Element konfiguracji CMDB</h2>
      {result.error ? <><PageState title="Nie udało się odczytać powiązania" detail={result.error} tone="error" /><button className="button button--secondary mt-3" onClick={result.reload}>Spróbuj ponownie</button></> : result.data?.ci ? (
        <div className="mt-3 flex flex-wrap items-center gap-2 text-sm">
          <strong>{result.data.ci.name}</strong><span className="text-slate-500">{result.data.ci.identifier}</span>
          <span className="tag">{labels[result.data.ci.class] ?? result.data.ci.class}</span>
          <span className="tag">{labels[result.data.ci.status] ?? result.data.ci.status}</span>
        </div>
      ) : <p className="mt-2 text-sm text-slate-600">Ten zasób nie jest powiązany z CI.</p>}
    </section>
  );
}

type HistoryEntry = { id: string; actor_name: string; summary: string; event_time: string; data: unknown };
function AssetHistory({ id }: { id: string }) {
  const page = useRemotePage<HistoryEntry>(`/api/v1/cmdb/assets/${id}/history?limit=25`, "entries", "before_id");
  return <>
    <PageFeedback loading={page.loading} error={page.error} empty={!page.items.length} />
    {page.error && <button className="button button--secondary" onClick={page.retry}>Spróbuj ponownie</button>}
    {page.items.length > 0 && <ol className="space-y-4 border-l border-sn-line pl-5">{page.items.map((entry) => <li key={entry.id} className="relative"><span className="absolute -left-[1.55rem] top-1 h-2.5 w-2.5 rounded-full bg-sn-teal" /><strong>{entry.summary}</strong><p className="text-sm text-slate-500">{entry.actor_name} · {new Date(entry.event_time).toLocaleString("pl-PL")}</p><details className="mt-2 text-sm"><summary className="cursor-pointer text-sn-teal">Szczegóły zmiany</summary><pre className="mt-2 overflow-x-auto rounded bg-slate-50 p-3 text-xs">{JSON.stringify(entry.data, null, 2)}</pre></details></li>)}</ol>}
    <More loading={page.loading} next={page.next} load={() => void page.more()} />
  </>;
}
