import { Link, useSearchParams } from "react-router-dom";
import type { ReactNode } from "react";
import { useAuth } from "../auth/AuthContext";
import { PageState } from "../components/PageState";
import { useRemotePage } from "../components/useRemotePage";
import { useIncidentTitleFocus } from "../incidents/hooks";
import type { IncidentSummary } from "../incidents/contracts";
import type { Request, Publication } from "../requests/contracts";
import type { Asset } from "../cmdb/contracts";
import type { TenantUser } from "../api/types";

type SearchItem = IncidentSummary | Request | Publication | Asset | TenantUser;

export function GlobalSearchPage() {
  const heading = useIncidentTitleFocus();
  const [params] = useSearchParams();
  const query = (params.get("q") ?? "").trim();
  const { hasPermission } = useAuth();
  if (!query) return <PageState title="Wpisz frazę w wyszukiwarce globalnej" detail="Wyszukiwarka znajduje dostępne zgłoszenia, zasoby, usługi katalogowe i użytkowników." />;
  if (Array.from(query).length > 160) return <PageState title="Fraza jest za długa" detail="Wyszukiwanie obsługuje maksymalnie 160 znaków." tone="error" />;

  const q = new URLSearchParams({ q: query, limit: "10" });
  const buckets: ReactNode[] = [];
  if (hasPermission("incident.read") || hasPermission("incident.read_own")) {
    buckets.push(<SearchBucket<IncidentSummary> key="incidents" title="Incydenty" path={`/api/v1/incidents/?${q}`} collection="incidents" cursor="after_id" allTo={`/incidents?q=${encodeURIComponent(query)}`} render={(item) => <Link to={`/incidents/${item.id}`}><strong>{item.number}</strong><span>{item.title}</span><small>{item.state} · P{item.priority}</small></Link>} />);
  }
  if (hasPermission("request.read_own") || hasPermission("request.manage")) {
    const view = hasPermission("request.manage") ? "all" : "mine";
    const requestQuery = new URLSearchParams({ q: query, limit: "10", view });
    buckets.push(<SearchBucket<Request> key="requests" title="Wnioski" path={`/api/v1/requests/?${requestQuery}`} collection="items" cursor="before_id" allTo={`/requests?q=${encodeURIComponent(query)}&view=${view}`} render={(item) => <Link to={`/requests/${item.id}`}><strong>{item.number}</strong><span>{item.items.map((entry) => entry.snapshot.title).join(", ") || "Wniosek o usługę"}</span><small>{item.state}</small></Link>} />);
  }
  if (hasPermission("catalog.read")) {
    buckets.push(<SearchBucket<Publication> key="catalog" title="Katalog usług" path={`/api/v1/catalog/items?${q}`} collection="items" cursor="after_id" allTo={`/catalog?q=${encodeURIComponent(query)}`} render={(item) => <Link to={`/catalog/${item.id}`}><strong>{item.title}</strong><span>{item.description}</span><small>Usługa katalogowa</small></Link>} />);
  }
  if (hasPermission("assets.read") || hasPermission("assets.read_own")) {
    const assetQuery = new URLSearchParams({ q: query, limit: "10" });
    buckets.push(<SearchBucket<Asset> key="assets" title="Zasoby IT" path={`/api/v1/cmdb/assets?${assetQuery}`} collection="items" cursor="before_id" allTo={`/assets?q=${encodeURIComponent(query)}`} render={(item) => <Link to={`/assets/${item.id}`}><strong>{item.asset_tag} · {item.name}</strong><span>{[item.vendor, item.model, item.serial_number].filter(Boolean).join(" · ")}</span><small>{item.owner_name} · {item.state}{item.archived ? " · Archiwum" : ""}</small></Link>} />);
  }
  if (hasPermission("admin.users.manage")) {
    buckets.push(<SearchBucket<TenantUser> key="users" title="Użytkownicy" path={`/api/v1/admin/users/?${q}`} collection="users" cursor="after_id" allTo={`/users?q=${encodeURIComponent(query)}`} render={(item) => <Link to={`/users?q=${encodeURIComponent(query)}`}><strong>{item.display_name}</strong><span>{item.email}</span><small>{item.membership_active ? "Aktywne członkostwo" : "Nieaktywne członkostwo"}</small></Link>} />);
  }

  return (
    <section className="page">
      <div className="page-heading">
        <div><p className="eyebrow">Wyszukiwanie globalne</p><h1 ref={heading} tabIndex={-1}>Wyniki wyszukiwania</h1><p className="muted">Fraza: <strong className="text-slate-800">{query}</strong></p></div>
      </div>
      {buckets.length ? <div className="grid gap-4 xl:grid-cols-2">{buckets}</div> : <PageState title="Brak dostępnych obszarów wyszukiwania" detail="Konto nie ma uprawnień do odczytu żadnego obsługiwanego modułu." />}
    </section>
  );
}

function SearchBucket<T extends SearchItem>({ title, path, collection, cursor, allTo, render }: {
  title: string;
  path: string;
  collection: string;
  cursor: string;
  allTo: string;
  render: (item: T) => ReactNode;
}) {
  const page = useRemotePage<T>(path, collection, cursor);
  return (
    <section className="panel min-w-0">
      <div className="mb-3 flex items-center justify-between gap-3"><h2 className="text-lg font-semibold">{title}</h2><Link className="text-sm font-semibold text-sn-teal hover:underline" to={allTo}>Otwórz listę</Link></div>
      {page.loading && <PageState title={`Szukanie: ${title.toLocaleLowerCase("pl-PL")}…`} />}
      {page.error && <><PageState title={`Nie udało się przeszukać: ${title.toLocaleLowerCase("pl-PL")}`} detail={page.error} tone="error" /><button className="button button--secondary mt-3" onClick={page.retry}>Spróbuj ponownie</button></>}
      {!page.loading && !page.error && page.items.length === 0 && <p className="rounded-md bg-slate-50 p-3 text-sm text-slate-600">Brak dopasowań.</p>}
      {page.items.length > 0 && <ul className="divide-y divide-slate-100">{page.items.map((item, index) => <li key={"id" in item ? item.id : index} className="py-3 first:pt-0 last:pb-0 [&_a]:grid [&_a]:gap-1 [&_a]:rounded [&_a]:p-2 [&_a]:hover:bg-slate-50 [&_a]:focus-visible:outline-2 [&_a]:focus-visible:outline-sn-teal [&_small]:text-slate-500">{render(item)}</li>)}</ul>}
      {page.next && <button className="button button--secondary mt-3" disabled={page.loading} onClick={() => void page.more()}>Pokaż więcej</button>}
    </section>
  );
}
