import { useState } from "react";

import { Icon } from "../../../shared/ui/Icon.jsx";

export function TitleEditor({ title, onRename }) {
  const [editing, setEditing] = useState(false);
  const [value, setValue] = useState(title);
  const [busy, setBusy] = useState(false);

  if (!editing) {
    return (
      <div className="title-line">
        <h3>{title}</h3>
        <button
          className="icon-btn icon-only"
          title="Переименовать"
          aria-label="Переименовать дашборд"
          onClick={() => {
            setValue(title);
            setEditing(true);
          }}
        >
          <Icon name="edit" size={14} />
        </button>
      </div>
    );
  }

  const submit = async (e) => {
    e.preventDefault();
    const next = value.trim();
    if (!next || next === title) {
      setEditing(false);
      return;
    }
    setBusy(true);
    try {
      await onRename(next);
      setEditing(false);
    } finally {
      setBusy(false);
    }
  };

  return (
    <form className="title-line editing" onSubmit={submit}>
      <input
        autoFocus
        value={value}
        maxLength={200}
        aria-label="Название дашборда"
        onChange={(e) => setValue(e.target.value)}
        onKeyDown={(e) => e.key === "Escape" && setEditing(false)}
      />
      <button className="icon-btn" type="submit" disabled={busy || !value.trim()}>
        Сохранить
      </button>
      <button className="icon-btn" type="button" onClick={() => setEditing(false)}>
        Отмена
      </button>
    </form>
  );
}
