import { useState } from "react";

import { PeriodBar } from "./components/PeriodBar.jsx";
import { Summary } from "./components/Summary.jsx";
import { TitleEditor } from "./components/TitleEditor.jsx";
import { TrashPanel } from "./components/TrashPanel.jsx";
import { UndoToast } from "./components/UndoToast.jsx";
import { Widget } from "./components/Widget.jsx";
import { useDashboards } from "./hooks/useDashboards.js";
import { fmtDay } from "./lib/period.js";
import { fmtDateTime } from "../../shared/lib/format.js";
import { plural } from "../../shared/lib/plural.js";
import { EmptyState } from "../../shared/ui/EmptyState.jsx";
import { ErrorBanner } from "../../shared/ui/ErrorBanner.jsx";
import { Icon } from "../../shared/ui/Icon.jsx";
import { Spinner } from "../../shared/ui/Spinner.jsx";
import "./styles.css";

export function DashboardsPage({ active, theme, openId, onOpen, onCount }) {
  const {
    items, current, error, loading, period, setPeriod, reload, refresh, remove, removeWidget, rename, exporting, exportFile, exportImage,
    trash, loadTrash, undo, setUndo, restore,
  } = useDashboards({ openId, onCount });
  const [trashOpen, setTrashOpen] = useState(false);
  const bringBack = async (item) => {
    if (await restore(item)) onOpen(item.dashboard_id);
  };
  const widgets = current?.widgets || [];

  return (
    <section className={`view${active ? " active" : ""}`}>
      <div className="view-head">
        <h2>Дашборды</h2>
        <p>Ваши показатели и визуализации — всегда под рукой</p>
        <button
          className="icon-btn trash-toggle"
          aria-expanded={trashOpen}
          onClick={() => {
            if (!trashOpen) loadTrash();
            setTrashOpen(!trashOpen);
          }}
        >
          <Icon name="trash" size={14} /> Корзина{trash.length ? ` (${trash.length})` : ""}
        </button>
      </div>

      <div className="scroll">
        <div className="col-wide">
          {items.length ? (
            <nav className="dash-list" aria-label="Сохранённые дашборды">
              {items.map((d) => (
                <button
                  key={d.id}
                  className={`dash-card${d.id === openId ? " active" : ""}`}
                  aria-current={d.id === openId ? "page" : undefined}
                  onClick={() => onOpen(d.id)}
                >
                  <span className="dash-card-title">{d.title}</span>
                  <span className="dash-card-meta">
                    {`${(d.widgets || []).length} ${plural((d.widgets || []).length, "виджет", "виджета", "виджетов")} · ${fmtDateTime(d.created_at)}`}
                  </span>
                </button>
              ))}
            </nav>
          ) : null}

          <ErrorBanner error={error} onRetry={reload} />

          {trashOpen ? <TrashPanel items={trash} onRestore={bringBack} /> : null}

          {!items.length ? (
            <EmptyState title="Панелей пока нет">
              Попросите агента в чате: «собери дашборд с ежеквартальной статистикой».
            </EmptyState>
          ) : null}

          {items.length && !openId ? (
            <EmptyState title="Выберите дашборд">Откройте панель из списка выше — показатели пересчитаются по свежим данным.</EmptyState>
          ) : null}

          {current ? (
            <div className="dashboard">
              <div className="ds-head">
                <div className="ds-title">
                  <TitleEditor title={current.title} onRename={(title) => rename(current.id, title)} />
                  <div className="ds-meta">
                    {current.description ? `${current.description} · ` : ""}
                    {`собран ${fmtDateTime(current.created_at)}`}
                    {current.refreshed_at ? ` · обновлён ${fmtDateTime(current.refreshed_at)}` : ""}
                    {current.period ? ` · период ${fmtDay(current.period.from) || "…"} — ${fmtDay(current.period.to) || "…"}` : ""}
                  </div>
                </div>
                <div className="ds-actions">
                  <button className="icon-btn" onClick={refresh} disabled={loading}>
                    <Icon name="refresh" size={14} /> Обновить
                  </button>
                  <button
                    className="icon-btn"
                    onClick={async () => {
                      if (!window.confirm(`Удалить дашборд «${current.title}»? Его можно будет вернуть из корзины в течение суток.`)) return;
                      await remove(current.id, current.title);
                      onOpen(null);
                    }}
                  >
                    Удалить
                  </button>
                </div>
              </div>

              <PeriodBar range={current.date_range} period={period} onChange={setPeriod} />

              {loading ? <Spinner label="пересчитываю показатели…" /> : null}

              <Summary widgets={widgets} />

              {widgets.length ? (
                <div className={`dash-grid${loading ? " stale" : ""}`}>
                  {widgets.map((w) => (
                    <Widget
                      key={w.id || w.title}
                      widget={w}
                      theme={theme}
                      filtered={!!current.period}
                      exporting={!!exporting && exporting.endsWith(`:${w.id}`)}
                      onExportImage={w.id && !w.error ? (node) => exportImage(node, `${current.title} — ${w.title}`, `png:${w.id}`) : undefined}
                      onExportTable={w.id && !w.error ? (format) => exportFile(format, w.id) : undefined}
                      onRemove={async () => {
                        if (!window.confirm(`Убрать виджет «${w.title}» с дашборда? Его можно будет вернуть из корзины в течение суток.`)) return;
                        await removeWidget(current.id, w.id, w.title);
                      }}
                    />
                  ))}
                </div>
              ) : (
                <EmptyState title="На дашборде пока пусто">
                  Закрепите ответ из чата кнопкой «Закрепить на дашборде» или попросите агента:
                  «добавь на дашборд выручку по месяцам».
                </EmptyState>
              )}
            </div>
          ) : loading ? (
            <Spinner label="пересчитываю показатели…" />
          ) : null}
        </div>
      </div>

      {undo ? <UndoToast item={undo} onRestore={() => bringBack(undo)} onClose={() => setUndo(null)} /> : null}
    </section>
  );
}
