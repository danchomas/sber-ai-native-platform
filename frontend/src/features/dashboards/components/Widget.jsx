import { Chart } from "../../../shared/charts/Chart.jsx";
import { Headline } from "../../../shared/ui/Headline.jsx";
import { Icon } from "../../../shared/ui/Icon.jsx";

export function Widget({ widget, theme, filtered, onRemove }) {
  const span = Math.min(Math.max(widget.width || 6, 1), 12);
  const spec = widget.chart ? { ...widget.chart, title: "" } : { type: "table" };
  const showHeadline = widget.headline && widget.chart?.type !== "kpi";

  return (
    <article className="widget" style={{ gridColumn: `span ${span}` }}>
      <header className="widget-head">
        <h4 title={widget.title}>{widget.title}</h4>
        {filtered && !widget.period_applied && !widget.error ? (
          <span className="badge" title="У показателя нет разбивки по времени, поэтому фильтр периода к нему не применяется">
            за весь период
          </span>
        ) : null}
        {onRemove ? (
          <button
            className="icon-btn icon-only widget-remove"
            onClick={onRemove}
            title="Убрать с дашборда"
            aria-label={`Убрать виджет «${widget.title}»`}
          >
            <Icon name="close" size={13} />
          </button>
        ) : null}
      </header>

      {widget.error ? (
        <div className="notice error">
          <span>✕</span>
          <span>{widget.error}</span>
        </div>
      ) : (
        <>
          {showHeadline ? <Headline headline={widget.headline} compact /> : null}
          <Chart spec={spec} result={widget.result} height={260} theme={theme} />
        </>
      )}
    </article>
  );
}
