import { useEffect, useState } from "react";

import { api } from "../../../shared/api/PlatformApi.js";
import { ErrorBanner } from "../../../shared/ui/ErrorBanner.jsx";
import { Modal } from "../../../shared/ui/Modal.jsx";

const NEW = "new";

// PinDialog закрепляет ответ на дашборде: на существующем или на новом.
//
// На панель уходят параметры запроса, а не выгрузка: виджет пересчитается
// при каждом открытии, а бэкенд проверит запрос ещё до сохранения.
export function PinDialog({ widget, onClose, onPinned }) {
  const [dashboards, setDashboards] = useState([]);
  const [target, setTarget] = useState(NEW);
  const [title, setTitle] = useState(widget.title);
  const [boardTitle, setBoardTitle] = useState("Мои показатели");
  const [error, setError] = useState(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    let alive = true;
    api
      .listDashboards()
      .then((list) => {
        if (!alive) return;
        setDashboards(list);
        if (list.length) setTarget(list[0].id);
      })
      .catch((err) => alive && setError(err));
    return () => {
      alive = false;
    };
  }, []);

  const submit = async () => {
    setBusy(true);
    setError(null);
    try {
      const payload = { ...widget, title: title.trim() };
      let id = target;
      if (target === NEW) {
        const created = await api.createDashboard(boardTitle.trim(), [payload]);
        id = created.id;
      } else {
        await api.addWidget(target, payload);
      }
      onPinned?.(id);
      onClose();
    } catch (err) {
      setError(err);
    } finally {
      setBusy(false);
    }
  };

  const ready = title.trim() && (target !== NEW || boardTitle.trim());

  return (
    <Modal
      title="Закрепить на дашборде"
      onClose={onClose}
      footer={
        <>
          <button className="icon-btn" onClick={onClose}>Отмена</button>
          <button className="send" disabled={busy || !ready} onClick={submit}>
            {busy ? "Сохраняю…" : "Закрепить"}
          </button>
        </>
      }
    >
      <div className="field">
        <label htmlFor="pin-title">Заголовок виджета</label>
        <input id="pin-title" value={title} onChange={(e) => setTitle(e.target.value)} />
      </div>

      <div className="field">
        <label>Куда</label>
        <div className="chips" role="radiogroup">
          {dashboards.map((d) => (
            <button
              key={d.id}
              role="radio"
              aria-checked={target === d.id}
              className={`chip${target === d.id ? " active" : ""}`}
              onClick={() => setTarget(d.id)}
            >
              {d.title}
            </button>
          ))}
          <button
            role="radio"
            aria-checked={target === NEW}
            className={`chip${target === NEW ? " active" : ""}`}
            onClick={() => setTarget(NEW)}
          >
            + Новый дашборд
          </button>
        </div>
      </div>

      {target === NEW ? (
        <div className="field">
          <label htmlFor="pin-board">Название дашборда</label>
          <input
            id="pin-board"
            value={boardTitle}
            onChange={(e) => setBoardTitle(e.target.value)}
            onKeyDown={(e) => e.key === "Enter" && ready && submit()}
          />
        </div>
      ) : null}

      <p className="hint">Виджет хранит запрос, а не цифры: при открытии дашборда показатели пересчитаются.</p>
      <ErrorBanner error={error} />
    </Modal>
  );
}
