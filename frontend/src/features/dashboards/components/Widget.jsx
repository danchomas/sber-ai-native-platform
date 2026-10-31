import { useEffect, useRef, useState } from "react";

import { Chart } from "../../../shared/charts/Chart.jsx";
import { Headline } from "../../../shared/ui/Headline.jsx";
import { Icon } from "../../../shared/ui/Icon.jsx";

const TABLE_FORMATS = [
  { format: "xlsx", label: "Excel (.xlsx)" },
  { format: "csv", label: "CSV (.csv)" },
];

function TableExportMenu({ title, disabled, onSelect }) {
  const [open, setOpen] = useState(false);
  const root = useRef(null);

  useEffect(() => {
    if (!open) return undefined;
    const onPointer = (e) => {
      if (!root.current?.contains(e.target)) setOpen(false);
    };
    const onKey = (e) => {
      if (e.key === "Escape") setOpen(false);
    };
    document.addEventListener("mousedown", onPointer);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onPointer);
      document.removeEventListener("keydown", onKey);
    };
  }, [open]);

  return (
    <div className={`export-menu${open ? " open" : ""}`} ref={root}>
      <button
        className="icon-btn icon-only widget-export-table"
        onClick={() => setOpen(!open)}
        disabled={disabled}
        aria-haspopup="menu"
        aria-expanded={open}
        title="Скачать таблицу, по которой построен виджет"
        aria-label={`Скачать таблицу виджета «${title}»: выбрать формат`}
      >
        <Icon name="tableDownload" size={16} />
      </button>
      {open ? (
        <div className="export-menu-list" role="menu" aria-label="Формат таблицы">
          {TABLE_FORMATS.map(({ format, label }) => (
            <button
              key={format}
              role="menuitem"
              className={`export-menu-item export-${format}`}
              onClick={() => {
                setOpen(false);
                onSelect(format);
              }}
            >
              {label}
            </button>
          ))}
        </div>
      ) : null}
    </div>
  );
}

export function Widget({ widget, theme, filtered, onRemove, onExportImage, onExportTable, exporting }) {
  const span = Math.min(Math.max(widget.width || 6, 1), 12);
  const spec = widget.chart ? { ...widget.chart, title: "" } : { type: "table" };
  const showHeadline = widget.headline && widget.chart?.type !== "kpi";
  const card = useRef(null);

  return (
    <article className="widget" ref={card} style={{ gridColumn: `span ${span}` }}>
      <header className="widget-head">
        <h4 title={widget.title}>{widget.title}</h4>
        {filtered && !widget.period_applied && !widget.error ? (
          <span className="badge" title="У показателя нет разбивки по времени, поэтому фильтр периода к нему не применяется">
            за весь период
          </span>
        ) : null}
        {onExportImage || onExportTable || onRemove ? (
          <div className="widget-tools">
            {onExportImage ? (
              <button
                className="icon-btn icon-only widget-export-image"
                onClick={() => onExportImage(card.current)}
                disabled={exporting}
                title="Скачать картинку виджета (PNG)"
                aria-label={`Скачать виджет «${widget.title}» картинкой`}
              >
                <Icon name="imageDownload" size={16} />
              </button>
            ) : null}
            {onExportTable ? <TableExportMenu title={widget.title} disabled={exporting} onSelect={onExportTable} /> : null}
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
          </div>
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
