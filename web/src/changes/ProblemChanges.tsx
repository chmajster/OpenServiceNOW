import { Link } from "react-router-dom";
import { useAuth } from "../auth/AuthContext";
import { useRemotePage } from "../components/useRemotePage";
import { More, PageFeedback } from "../cmdb/components";
import { labels, type Change } from "./contracts";
export function ProblemChanges({
  id,
  canLink,
}: {
  id: string;
  canLink: boolean;
}) {
  const { hasPermission } = useAuth(),
    read = ["change.read", "change.read_own"].some(hasPermission),
    p = useRemotePage<Change>(
      `/api/v1/changes/?problem_id=${id}&limit=25`,
      "items",
      "before_id",
      read,
    );
  return read ? (
    <section>
      <h3>Powiązane zmiany naprawcze</h3>
      <p>
        Powiązania zachowują niezależne stany problemu i zmiany. Zmianę dodasz
        lub odłączysz na jej zakładce Powiązane rekordy.
      </p>
      <PageFeedback
        loading={p.loading}
        error={p.error}
        empty={!p.items.length}
      />
      <ul>
        {p.items.map((c) => (
          <li key={c.id}>
            <Link to={`/changes/${c.id}`}>
              {c.number} · {c.title}
            </Link>{" "}
            · {labels[c.state]} · {labels[c.risk]}
          </li>
        ))}
      </ul>
      <More loading={p.loading} next={p.next} load={p.more} />
      {canLink && hasPermission("change.create") && (
        <Link className="button" to={`/changes/new?source_problem_id=${id}`}>
          Utwórz zmianę naprawczą z podglądem
        </Link>
      )}
    </section>
  ) : null;
}
