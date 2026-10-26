import { Widget } from "./components/Widget.jsx";
import { useDashboards } from "./hooks/useDashboards.js";
import { fmtDateTime } from "../../shared/lib/format.js";
import { EmptyState } from "../../shared/ui/EmptyState.jsx";
import { ErrorBanner } from "../../shared/ui/ErrorBanner.jsx";
import { Spinner } from "../../shared/ui/Spinner.jsx";
import "./styles.css";

export function DashboardsPage({ active, theme, openId, onOpen, onCount }) {
  const { items, current, error, loading, reload, refresh, remove, removeWidget, rename } =
    useDashboards({ openId, onCount });

  return (
    <section className={`view${active ? " active" : ""}`}>
      <div className="view-head">
        <h2>Дашборды</h2>
        <p>Ваши показатели и визуализации — всегда под рукой</p>
      </div>

      <div className="scroll">
        <div className="col-wide">
          <div className="chips" style={{ marginBottom: 18 }}>
            {items.map((d) => (
              <button
                key={d.id}
                className={`chip${d.id === openId ? " active" : ""}`}
                onClick={() => onOpen(d.id)}
              >
                {d.title}
              </button>
            ))}
          </div>

          <ErrorBanner error={error} onRetry={reload} />

          {!items.length ? (
            <EmptyState title="Панелей пока нет">
              Попросите агента в чате: «собери дашборд с ежеквартальной статистикой».
            </EmptyState>
          ) : null}

          {loading ? <Spinner label="пересчитываю показатели…" /> : null}

          {current ? (
            <div>
              <div className="ds-head">
                <div>
                  <h3>{current.title}</h3>
                  <div className="ds-meta">
                    {current.description ? `${current.description} · ` : ""}
                    собран {fmtDateTime(current.created_at)}
                  </div>
                </div>
                <div className="ds-actions">
                  <button className="icon-btn" onClick={refresh}>
                    Обновить
                  </button>
                  <button
                    className="icon-btn"
                    onClick={async () => {
                      const title = window.prompt("Новое название дашборда", current.title);
                      if (title && title.trim() && title.trim() !== current.title) await rename(current.id, title.trim());
                    }}
                  >
                    Переименовать
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

              {(current.widgets || []).length ? (
                <div className="dash-grid">
                  {current.widgets.map((w) => (
                    <Widget
                      key={w.id || w.title}
                      widget={w}
                      theme={theme}
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
          ) : null}
        </div>
      </div>
    </section>
  );
}
