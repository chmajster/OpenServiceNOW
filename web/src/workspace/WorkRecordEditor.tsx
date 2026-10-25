import { CoordinationActionEditor } from "../incidents/CoordinationActionEditor";
import { useCallback } from "react";
import { useAuth } from "../auth/AuthContext";
import { PageState } from "../components/PageState";
import { IncidentWorkspace } from "../incidents/IncidentWorkspace";
import { useIncidentQuery } from "../incidents/hooks";
import type { Incident, Metadata } from "../incidents/contracts";
import { RequestsPage } from "../pages/RequestsPage";
import type { WorkReference } from "./contracts";

export function WorkRecordEditor({
  reference,
  onDirtyChange,
}: {
  reference: WorkReference & { key: string };
  onDirtyChange: (key: string, dirty: boolean) => void;
}) {
  const reportDirty = useCallback(
    (dirty: boolean) => onDirtyChange(reference.key, dirty),
    [onDirtyChange, reference.key],
  );

  if(reference.object_type==="incident_action")return <CoordinationActionEditor id={reference.object_id} onDirtyChange={reportDirty}/>;
 if (reference.object_type === "incident") {
    return (
      <IncidentRecordEditor
        id={reference.object_id}
        onDirtyChange={reportDirty}
      />
    );
  }

  return (
    <RequestsPage
      tasks
      recordIDOverride={reference.object_id}
      onDirtyChange={reportDirty}
    />
  );
}

function IncidentRecordEditor({
  id,
  onDirtyChange,
}: {
  id: string;
  onDirtyChange: (dirty: boolean) => void;
}) {
  const { hasPermission } = useAuth();
  const metadata = useIncidentQuery<Metadata>("/api/v1/incidents/metadata");
  const incident = useIncidentQuery<Incident>(`/api/v1/incidents/${id}`);

  if (!hasPermission("incident.read") && !hasPermission("incident.read_own")) {
    return (
      <PageState
        title="Brak dostępu do incydentu"
        detail="Uprawnienia mogły zostać zmienione. Zamknij zakładkę lub odśwież listę."
        tone="error"
      />
    );
  }

  if (metadata.loading || incident.loading) {
    return <PageState title="Ładowanie formularza incydentu…" />;
  }

  if (!metadata.data || !incident.data) {
    const detail = metadata.error ?? incident.error ?? "Nie udało się odczytać rekordu.";
    return (
      <section className="panel" role="alert">
        <PageState title="Nie udało się otworzyć formularza incydentu" detail={detail} tone="error" />
        <button
          className="button button--secondary"
          type="button"
          onClick={() => {
            metadata.reload();
            incident.reload();
          }}
        >
          Spróbuj ponownie
        </button>
      </section>
    );
  }

  return (
    <IncidentWorkspace
      initial={incident.data}
      metadata={metadata.data}
      embedded
      onDirtyChange={onDirtyChange}
    />
  );
}
