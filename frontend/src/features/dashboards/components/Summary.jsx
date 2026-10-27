import { fmtCompact } from "../../../shared/lib/format.js";

// Summary — полоса «Главное» над сеткой: по одному числу с каждого
// виджета, у которого оно есть. Руководитель видит итог панели, не
// разбирая графики.
export function Summary({ widgets }) {
  const items = widgets.filter((w) => w.headline && !w.error).slice(0, 6);
  if (items.length < 2) return null;

  return (
    <section className="summary" aria-label="Главное на панели">
      {items.map((w) => {
        const change = w.headline.change_pct;
        return (
          <div className="summary-item" key={w.id || w.title}>
            <div className="summary-title" title={w.title}>{w.title}</div>
            <div className="summary-value">{fmtCompact(w.headline.value)}</div>
            <div className="summary-note">
              {change !== undefined && change !== null ? (
                <span className={`delta ${change > 0 ? "up" : change < 0 ? "down" : "flat"}`}>
                  {change > 0 ? "▲" : change < 0 ? "▼" : "→"}{" "}
                  {`${Math.abs(change).toLocaleString("ru-RU", { maximumFractionDigits: 1 })}%`}
                </span>
              ) : null}
              <span>{w.headline.label}</span>
            </div>
          </div>
        );
      })}
    </section>
  );
}
