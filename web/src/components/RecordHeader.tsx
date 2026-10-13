import type { ReactNode, Ref } from "react";
export function RecordHeader({
  number,
  title,
  type,
  state,
  version,
  assignment,
  severity,
  actions,
  headingRef,
}: {
  number: string;
  title: string;
  type: string;
  state: string;
  version: string;
  assignment?: string;
  severity?: ReactNode;
  actions?: ReactNode;
  headingRef?: Ref<HTMLHeadingElement>;
}) {
  return (
    <header className="page-heading record-heading">
      <div>
        <p className="eyebrow">
          {type} · {number}
        </p>
        <h1 ref={headingRef} tabIndex={-1}>
          {title}
        </h1>
        <div className="record-facts">
          <strong>{state}</strong>
          {severity}
          <span>Wersja {version}</span>
          {assignment && <span>{assignment}</span>}
        </div>
      </div>
      <div className="action-row">{actions}</div>
    </header>
  );
}
