import { VIEWS } from "../navigation.js";
import { fmtDateTime } from "../../shared/lib/format.js";
import { Icon } from "../../shared/ui/Icon.jsx";

export function Sidebar({
  view,
  onView,
  counts,
  sessions,
  activeSession,
  onSelectSession,
  onNewSession,
  health,
  onToggleTheme,
  theme,
}) {
  return (
    <aside className="sidebar">
      <div className="brand">
        <div className="brand-mark"><Icon name="sparkles" size={25} /></div>
        <div>
          <div className="brand-title">AI-Native BI</div>
          <div className="brand-sub">ваш AI-аналитик</div>
        </div>
      </div>

      <button className="new-chat" onClick={onNewSession}>
        <Icon name="plus" size={18} /> Новый диалог
      </button>

      <nav className="nav" aria-label="Разделы платформы">
        {VIEWS.map((v) => (
          <button
            key={v.id}
            aria-current={view === v.id ? "page" : "false"}
            onClick={() => onView(v.id)}
          >
            <Icon name={v.icon} />
            <span className="nav-label">{v.title}</span>
            <span className="count">{counts[v.id] || ""}</span>
          </button>
        ))}
      </nav>

      <div className="side-section">
        <div className="side-head">История диалогов</div>
        <div className="side-list">
          {!sessions.length ? <p className="side-empty">Здесь появятся ваши диалоги с аналитиком</p> : null}
          {sessions.map((s) => (
            <button
              key={s.id}
              className={`side-item${s.id === activeSession ? " active" : ""}`}
              title={s.title}
              onClick={() => onSelectSession(s.id)}
            >
              {s.title}
              <time>{fmtDateTime(s.updated_at)}</time>
            </button>
          ))}
        </div>
      </div>

      <div className="side-footer">
        <button className="theme-toggle" onClick={onToggleTheme} aria-label="Сменить тему" title="Сменить тему">
          <Icon name="theme" size={17} />
          <span>{theme === "dark" ? "Светлая тема" : "Тёмная тема"}</span>
          <Icon name="chevron" size={13} />
        </button>
        <div className="status" role="status">
          <span className={`dot ${health.tone}`} />
          <span title={health.details ? JSON.stringify(health.details, null, 2) : undefined}>{health.text}</span>
        </div>
      </div>
    </aside>
  );
}
