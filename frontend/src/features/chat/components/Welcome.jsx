import { Icon } from "../../../shared/ui/Icon.jsx";

const EXAMPLES = [
  { icon: "data", label: "Разобраться в данных", text: "Что есть в этих данных?", caption: "Структура, показатели и главное" },
  { icon: "dashboards", label: "Найти закономерности", text: "Покажи динамику по месяцам", caption: "Тренды и изменения во времени" },
  { icon: "check", label: "Проверить качество", text: "Есть ли здесь повторяющиеся записи?", caption: "Пропуски, повторы и отклонения" },
  { icon: "sparkles", label: "Собрать дашборд", text: "Собери дашборд с ежеквартальной статистикой", caption: "Наглядная история ваших данных" },
];

export function Welcome({ onAsk, disabled }) {
  return (
    <div className="welcome">
      <div className="welcome-symbol" aria-hidden="true"><Icon name="sparkles" size={35} /></div>
      <div className="welcome-eyebrow">Меньше рутины. Больше открытий.</div>
      <h1>Ваши данные.<br /><span>Новые возможности.</span></h1>
      <p className="welcome-description">
        Задавайте вопросы обычными словами.<br className="desktop-break" /> AI-аналитик найдёт ответы и превратит цифры в решения.
      </p>
      <div className="suggestion-label">С чего начнём?</div>
      <div className="suggestions">
        {EXAMPLES.map(({ icon, label, text, caption }) => (
          <button key={text} className="suggestion" onClick={() => onAsk(text)} disabled={disabled} title={text}>
            <span className={`suggestion-icon suggestion-icon-${icon}`}><Icon name={icon} size={20} /></span>
            <span className="suggestion-content"><strong>{label}</strong><span>{caption}</span></span>
            <Icon name="arrow" size={15} className="suggestion-arrow" />
          </button>
        ))}
      </div>
      <p className="welcome-note"><Icon name="sources" size={13} /> Ответы на основе загруженных файлов и подключённых баз</p>
    </div>
  );
}
