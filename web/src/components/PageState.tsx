import type { ReactNode } from "react";
type PageStateProps = {
  title: string;
  detail?: string;
  tone?: "neutral" | "error" | "success";
  action?: ReactNode;
};

export function PageState({
  title,
  detail,
  tone = "neutral",
  action,
}: PageStateProps) {
  return (
    <div
      className={`page-state page-state--${tone}`}
      role={tone === "error" ? "alert" : "status"}
    >
      <strong>{title}</strong>
      {detail ? <span>{detail}</span> : null}
      {action}
    </div>
  );
}
