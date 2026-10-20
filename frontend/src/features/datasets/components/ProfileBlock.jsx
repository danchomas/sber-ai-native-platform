import { Table } from "../../../shared/charts/Chart.jsx";
import { fmtNum } from "../../../shared/lib/format.js";

// ProfileBlock — сводка «что это за данные»: охват по времени, кандидаты
// в ключи, дубликаты, выведенные связи и пример строк.
export function ProfileBlock({ profile, theme }) {
  if (!profile) return null;

  const facts = [
    profile.duplicate_rows ? `дубликатов строк: ${fmtNum(profile.duplicate_rows)}` : "дубликатов нет",
    profile.candidate_keys?.length
      ? `похожи на ключ: ${profile.candidate_keys.join(", ")}`
      : null,
  ].filter(Boolean);

  return (
    <div>
      <div className="muted" style={{ marginBottom: 10 }}>{facts.join(" · ")}</div>

      {(profile.time_coverage || []).map((span) => (
        <div className="muted" key={span.column}>
          {span.column}: {span.from?.slice(0, 10)} … {span.to?.slice(0, 10)} ({span.days} дн.)
        </div>
      ))}

      {(profile.relations || []).length ? (
        <div style={{ marginTop: 10 }}>
          <div className="muted">Связи:</div>
          {profile.relations.map((r, i) => (
            <div className="muted" key={i}>
              «{r.left_dataset}».{r.left_column} → «{r.right_dataset}».{r.right_column} —
              пересечение {Math.round(r.overlap * 100)}%, тип {r.cardinality}
            </div>
          ))}
        </div>
      ) : null}

      {profile.sample ? (
        <div style={{ marginTop: 12 }}>
          <Table result={profile.sample} limit={10} theme={theme} />
        </div>
      ) : null}
    </div>
  );
}
