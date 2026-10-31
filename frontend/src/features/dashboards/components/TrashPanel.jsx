import { plural } from "../../../shared/lib/plural.js";

export function timeLeft(expiresAt, now = Date.now()) {
  const hours = Math.ceil((new Date(expiresAt).getTime() - now) / 3600000);
  if (hours <= 1) return "меньше часа";
  return `${hours} ${plural(hours, "час", "часа", "часов")}`;
}

function describe(item) {
  if (item.kind === "widget") return `виджет дашборда «${item.dashboard_title}»`;
  return `дашборд, ${item.widgets} ${plural(item.widgets, "виджет", "виджета", "виджетов")}`;
}

export function TrashPanel({ items, onRestore }) {
  return (
    <section className="trash-panel" aria-label="Корзина">
      <h3>Корзина</h3>
      <p className="trash-hint">Удалённое хранится сутки, затем стирается насовсем.</p>
      {items.length ? (
        <ul className="trash-list">
          {items.map((item) => (
            <li key={`${item.kind}:${item.id}`} className="trash-item">
              <div className="trash-info">
                <span className="trash-title">{item.title}</span>
                <span className="trash-meta">{`${describe(item)} · вернуть можно ещё ${timeLeft(item.expires_at)}`}</span>
              </div>
              <button className="icon-btn trash-restore" onClick={() => onRestore(item)}>Вернуть</button>
            </li>
          ))}
        </ul>
      ) : (
        <p className="trash-empty">Корзина пуста.</p>
      )}
    </section>
  );
}
