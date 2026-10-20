import { useState } from "react";

import { fmtNum } from "../../../shared/lib/format.js";
import { ErrorBanner } from "../../../shared/ui/ErrorBanner.jsx";
import { Spinner } from "../../../shared/ui/Spinner.jsx";

// SourceCard показывает подключённую базу и её таблицы.
// Подключить таблицу — значит завести по ней датасет: платформа
// прочитает структуру и посчитает профиль, данные останутся на месте.
export function SourceCard({ source, tables, onLoadTables, onAttachTable, onDetach }) {
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState(null);
  const [attaching, setAttaching] = useState(null);

  const toggle = async () => {
    if (open) {
      setOpen(false);
      return;
    }
    setBusy(true);
    setError(null);
    try {
      await onLoadTables(source.id);
      setOpen(true);
    } catch (err) {
      setError(err);
    } finally {
      setBusy(false);
    }
  };

  const attach = async (t) => {
    setAttaching(`${t.schema}.${t.name}`);
    setError(null);
    try {
      await onAttachTable(source.id, t.schema, t.name);
    } catch (err) {
      setError(err);
    } finally {
      setAttaching(null);
    }
  };

  return (
    <div className="card">
      <div className="ds-head">
        <div>
          <h3>
            {source.name}
            <span className={`tag ${source.status === "ready" ? "" : "warn"}`} style={{ marginLeft: 8 }}>
              {source.status === "ready" ? "доступен" : "недоступен"}
            </span>
          </h3>
          <div className="ds-meta">
            {source.kind}
            {source.fail_reason ? ` · ${source.fail_reason}` : ""}
          </div>
        </div>
        <div className="ds-actions">
          <button className="icon-btn" disabled={busy} onClick={toggle}>
            {open ? "Скрыть таблицы" : "Таблицы"}
          </button>
          <button
            className="icon-btn"
            onClick={async () => {
              if (!window.confirm(`Отключить «${source.name}»? Данные в базе останутся нетронутыми.`)) return;
              try {
                await onDetach(source.id);
              } catch (err) {
                setError(err);
              }
            }}
          >
            Отключить
          </button>
        </div>
      </div>

      <ErrorBanner error={error} />
      {busy ? <Spinner label="читаю структуру базы…" /> : null}

      {open ? (
        <div className="table-wrap" style={{ marginTop: 12 }}>
          <table className="data">
            <thead>
              <tr>
                <th>Схема</th>
                <th>Таблица</th>
                <th>Вид</th>
                <th className="num">Строк (оценка)</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {(tables || []).map((t) => (
                <tr key={`${t.schema}.${t.name}`}>
                  <td>{t.schema}</td>
                  <td>{t.name}</td>
                  <td>{t.kind === "view" ? "представление" : "таблица"}</td>
                  <td className="num">{fmtNum(t.estimate_rows)}</td>
                  <td>
                    {t.attached ? (
                      <span className="muted">подключена</span>
                    ) : (
                      <button
                        className="icon-btn"
                        disabled={attaching === `${t.schema}.${t.name}`}
                        onClick={() => attach(t)}
                      >
                        {attaching === `${t.schema}.${t.name}` ? "Профилирую…" : "Подключить"}
                      </button>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : null}
    </div>
  );
}
