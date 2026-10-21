import { useState } from "react";

import { AttachDialog } from "./components/AttachDialog.jsx";
import { SourceCard } from "./components/SourceCard.jsx";
import { useSources } from "./hooks/useSources.js";
import "./styles.css";
import { EmptyState } from "../../shared/ui/EmptyState.jsx";
import { ErrorBanner } from "../../shared/ui/ErrorBanner.jsx";
import { Spinner } from "../../shared/ui/Spinner.jsx";

export function SourcesPage({ active, onCatalogChanged }) {
  const sources = useSources();
  const [dialog, setDialog] = useState(false);

  const attachTable = async (id, schema, table) => {
    const dataset = await sources.attachTable(id, schema, table);
    onCatalogChanged?.();
    return dataset;
  };

  const detach = async (id) => {
    await sources.detach(id);
    onCatalogChanged?.();
  };

  return (
    <section className={`view${active ? " active" : ""}`}>
      <div className="view-head">
        <h2>Источники</h2>
        <p>Подключение к существующей базе — данные читаются на месте, без загрузки и преобразования</p>
      </div>

      <div className="scroll">
        <div className="grid col-wide">
          <div className="row">
            <button className="send" onClick={() => setDialog(true)}>
              Подключить базу
            </button>
          </div>

          <ErrorBanner error={sources.error} onRetry={sources.reload} />

          {sources.loading && !sources.items.length ? <Spinner label="читаю источники…" /> : null}

          {!sources.loading && !sources.items.length ? (
            <EmptyState title="Источников пока нет">
              Подключите PostgreSQL — платформа покажет её таблицы, а выбранные
              заведёт как датасеты. Файлы при этом загружать не нужно.
            </EmptyState>
          ) : null}

          {sources.items.map((s) => (
            <SourceCard
              key={s.id}
              source={s}
              tables={sources.tables[s.id]}
              onLoadTables={sources.loadTables}
              onAttachTable={attachTable}
              onDetach={detach}
            />
          ))}
        </div>
      </div>

      {dialog ? (
        <AttachDialog onClose={() => setDialog(false)} onAttach={sources.attach} />
      ) : null}
    </section>
  );
}
