type PageStateProps = {
  title: string;
  detail?: string;
  tone?: "neutral" | "error" | "success";
};

export function PageState({ title, detail, tone = "neutral" }: PageStateProps) {
  return (
    <div className={`page-state page-state--${tone}`} role={tone === "error" ? "alert" : "status"}>
      <strong>{title}</strong>
      {detail ? <span>{detail}</span> : null}
    </div>
  );
}
