import { Chart } from "../../../shared/charts/Chart.jsx";
import { Icon } from "../../../shared/ui/Icon.jsx";

// Widget занимает долю сетки из 12 колонок — ширину выбрал агент под тип
// визуализации: показатель узкий, динамика во всю ширину.
export function Widget({ widget, theme, onRemove }) {
  const span = Math.min(Math.max(widget.width || 6, 1), 12);

  return (
    <div className="widget" style={{ gridColumn: `span ${span}` }}>
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
      {widget.error ? (
        <div className="notice error">
          <span>✕</span>
          <span>{widget.title}: {widget.error}</span>
        </div>
      ) : (
        <Chart spec={widget.chart} result={widget.result} height={260} theme={theme} />
      )}
    </div>
  );
}
