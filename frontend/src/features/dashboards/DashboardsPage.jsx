import { PeriodBar } from "./components/PeriodBar.jsx";
import { Summary } from "./components/Summary.jsx";
import { TitleEditor } from "./components/TitleEditor.jsx";
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
  const { items, current, error, loading, period, setPeriod, reload, refresh, remove, removeWidget, rename } =
    useDashboards({ openId, onCount });
  const widgets = current?.widgets || [];

  return (
    <section className={`view${active ? " active" : ""}`}>
      <div className="view-head">
        <h2>Дашборды</h2>
        <p>Ваши показатели и визуализации — всегда под рукой</p>
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
                  <button className="icon-btn" onClick={() => window.print()} title="Напечатать или сохранить в PDF">
                    <Icon name="print" size={14} /> PDF
                  </button>
                  <button
                    className="icon-btn"
                    onClick={async () => {
                      if (!window.confirm(`Удалить дашборд «${current.title}»?`)) return;
                      await remove(current.id);
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
                      onRemove={async () => {
                        if (!window.confirm(`Убрать виджет «${w.title}» с дашборда?`)) return;
                        await removeWidget(current.id, w.id);
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
    </section>
  );
}
