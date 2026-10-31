import { Icon } from "../../../shared/ui/Icon.jsx";

export function UndoToast({ item, onRestore, onClose }) {
  const name = item.kind === "widget" ? "Виджет" : "Дашборд";
  return (
    <div className="undo-toast" role="status">
      <span className="undo-text">{`${name} «${item.title}» удалён`}</span>
      <button className="icon-btn undo-restore" onClick={onRestore}>Вернуть</button>
      <button className="icon-btn icon-only" onClick={onClose} title="Закрыть" aria-label="Закрыть уведомление">
        <Icon name="close" size={13} />
      </button>
    </div>
  );
}
