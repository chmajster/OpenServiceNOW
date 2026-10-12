import { PageState } from "../components/PageState";
export function LoadState({
  loading,
  error,
  retry,
}: {
  loading?: boolean;
  error?: string | null;
  retry?: () => void;
}) {
  return (
    <>
      {loading ? <PageState title="Ładowanie…" /> : null}
      {error ? (
        <>
          <PageState title={error} tone="error" />
          {retry ? (
            <button className="button button--secondary" onClick={retry}>
              Spróbuj ponownie
            </button>
          ) : null}
        </>
      ) : null}
    </>
  );
}
