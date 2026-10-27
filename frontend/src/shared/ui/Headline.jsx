import { fmtCompact } from "../lib/format.js";

// Headline показывает главное в результате: число, что это за число,
// и изменение к прошлому периоду или долю лидера. Всё посчитано
// платформой по строкам результата, поэтому совпадает с графиком.
export function Headline({ headline, compact = false }) {
  if (!headline) return null;
  const change = headline.change_pct;
  const share = headline.share_pct;

  return (
    <div className={`headline${compact ? " compact" : ""}`}>
      <div className="headline-value">{fmtCompact(headline.value)}</div>
      <div className="headline-meta">
        <span className="headline-label">{headline.label}</span>
        {change !== undefined && change !== null ? <ChangeBadge pct={change} /> : null}
        {share !== undefined && share !== null ? (
          <span className="headline-share">{`${String(share).replace(".", ",")}% от суммы`}</span>
        ) : null}
      </div>
    </div>
  );
}

function ChangeBadge({ pct }) {
  const direction = pct > 0 ? "up" : pct < 0 ? "down" : "flat";
  const arrow = pct > 0 ? "▲" : pct < 0 ? "▼" : "→";
  const value = `${Math.abs(pct).toLocaleString("ru-RU", { maximumFractionDigits: 1 })}%`;
  return (
    <span className={`delta ${direction}`} title="Изменение к предыдущему периоду">
      <span aria-hidden="true">{arrow}</span> {pct === 0 ? "без изменений" : value}
      <span className="sr-only">{pct > 0 ? " рост" : pct < 0 ? " снижение" : ""} к предыдущему периоду</span>
    </span>
  );
}
