import { Chart } from "../../../shared/charts/Chart.jsx";

// Widget занимает долю сетки из 12 колонок — ширину выбрал агент под тип
// визуализации: показатель узкий, динамика во всю ширину.
export function Widget({ widget, theme }) {
  const span = Math.min(Math.max(widget.width || 6, 1), 12);

  return (
    <div className="widget" style={{ gridColumn: `span ${span}` }}>
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
