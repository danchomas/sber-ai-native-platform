import { useState } from "react";

import { Modal } from "../../../shared/ui/Modal.jsx";
import { ErrorBanner } from "../../../shared/ui/ErrorBanner.jsx";

const PLACEHOLDER = "postgres://user:password@host:5432/dbname?sslmode=disable";

export function AttachDialog({ onClose, onAttach }) {
  const [name, setName] = useState("");
  const [dsn, setDsn] = useState("");
  const [error, setError] = useState(null);
  const [busy, setBusy] = useState(false);

  const submit = async () => {
    setBusy(true);
    setError(null);
    try {
      await onAttach(name.trim(), dsn.trim());
      onClose();
    } catch (err) {
      setError(err);
    } finally {
      setBusy(false);
    }
  };

  return (
    <Modal
      title="Подключить базу данных"
      onClose={onClose}
      footer={
        <>
          <button className="icon-btn" onClick={onClose}>Отмена</button>
          <button className="send" disabled={busy || !name.trim() || !dsn.trim()} onClick={submit}>
            {busy ? "Проверяю связь…" : "Подключить"}
          </button>
        </>
      }
    >
      <p className="muted" style={{ marginTop: 0 }}>
        Платформа читает таблицы там, где они лежат: данные не копируются
        и не преобразуются. Подключение проверяется до сохранения.
      </p>

      <div className="field">
        <label htmlFor="src-name">Название</label>
        <input
          id="src-name"
          value={name}
          placeholder="например, продажи"
          onChange={(e) => setName(e.target.value)}
        />
      </div>

      <div className="field">
        <label htmlFor="src-dsn">Строка подключения</label>
        <input
          id="src-dsn"
          value={dsn}
          placeholder={PLACEHOLDER}
          onChange={(e) => setDsn(e.target.value)}
          onKeyDown={(e) => e.key === "Enter" && submit()}
        />
        <p className="hint">Поддерживается PostgreSQL. Строка хранится в базе платформы и не возвращается через API.</p>
      </div>

      <ErrorBanner error={error} />
    </Modal>
  );
}
