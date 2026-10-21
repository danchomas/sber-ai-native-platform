import { VIEWS } from "../navigation.js";
import { fmtDateTime } from "../../shared/lib/format.js";
import { Icon } from "../../shared/ui/Icon.jsx";

// Sidebar — навигация, список диалогов и состояние платформы.
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
}) {
  return (
    <aside className="sidebar">
      <div className="brand">
        <div className="brand-mark">AI</div>
        <div>
          <div className="brand-title">AI-Native BI</div>
          <div className="brand-sub">диалог с данными</div>
        </div>
      </div>

      <nav className="nav">
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
        <div className="side-head">
          <span>Диалоги</span>
          <button className="icon-btn" onClick={onNewSession} title="Новый диалог">
            <Icon name="plus" size={14} />
            новый
          </button>
        </div>
        <div className="side-list">
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

      <div className="status">
        <span className={`dot ${health.tone}`} />
        <span title={health.details ? JSON.stringify(health.details, null, 2) : undefined}>
          {health.text}
        </span>
        <button
          className="icon-btn icon-only"
          style={{ marginLeft: "auto" }}
          onClick={onToggleTheme}
          title="Сменить тему"
          aria-label="Сменить тему"
        >
          <Icon name="theme" size={15} />
        </button>
      </div>
    </aside>
  );
}
