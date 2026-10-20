import { useState } from "react";

import { Table } from "../../../shared/charts/Chart.jsx";
import { fmtBytes, fmtNum } from "../../../shared/lib/format.js";
import { plural } from "../../../shared/lib/plural.js";
import { api } from "../../../shared/api/PlatformApi.js";
import { ErrorBanner } from "../../../shared/ui/ErrorBanner.jsx";
import { ProfileBlock } from "./ProfileBlock.jsx";

const ROLE_LABELS = {
  identifier: "идентификатор",
  temporal: "дата",
  measure: "метрика",
  dimension: "разрез",
  free_text: "текст",
  unknown: "—",
};

function summary(dataset) {
  const cols = dataset.columns || [];
  const period = cols
    .filter((c) => c.role === "temporal" && c.profile?.min_value)
    .map((c) => `${c.profile.min_value} … ${c.profile.max_value}`)[0];

  return [
    `${fmtNum(dataset.row_count)} ${plural(dataset.row_count, "строка", "строки", "строк")}`,
    `${cols.length} ${plural(cols.length, "колонка", "колонки", "колонок")}`,
    dataset.size_bytes ? fmtBytes(dataset.size_bytes) : null,
    dataset.origin === "attached"
      ? `источник «${dataset.source_name}», таблица ${dataset.schema}.${dataset.table_name}`
      : `разделитель «${dataset.delimiter}»`,
    period ? `период ${period}` : null,
  ]
    .filter(Boolean)
    .join(" · ");
}

export function DatasetCard({ dataset, theme, onDelete }) {
  const [panel, setPanel] = useState(null);
  const [error, setError] = useState(null);
  const [busy, setBusy] = useState(false);

  const attached = dataset.origin === "attached";

  const show = async (kind) => {
    if (panel?.kind === kind) {
      setPanel(null);
      return;
    }
    setBusy(true);
    setError(null);
    try {
      const data =
        kind === "preview"
          ? await api.datasetPreview(dataset.id, 25)
          : await api.datasetProfile(dataset.id);
      setPanel({ kind, data });
    } catch (err) {
      setError(err);
    } finally {
      setBusy(false);
    }
  };

  const remove = async () => {
    const what = attached
      ? `Отключить «${dataset.name}»? Таблица в источнике останется нетронутой.`
      : `Удалить датасет «${dataset.name}» вместе с таблицей?`;
    if (!window.confirm(what)) return;
    try {
      await onDelete(dataset.id);
    } catch (err) {
      setError(err);
    }
  };

  return (
    <div className="card">
      <div className="ds-head">
        <div>
          <h3>
            {dataset.name}
            {attached ? <span className="tag" style={{ marginLeft: 8 }}>подключена</span> : null}
          </h3>
          <div className="ds-meta">{summary(dataset)}</div>
        </div>
        <div className="ds-actions">
          <button className="icon-btn" disabled={busy} onClick={() => show("preview")}>
            Первые строки
          </button>
          <button className="icon-btn" disabled={busy} onClick={() => show("profile")}>
            Профиль
          </button>
          <button className="icon-btn" onClick={remove}>
            {attached ? "Отключить" : "Удалить"}
          </button>
        </div>
      </div>

      <div className="tags">
        {(dataset.columns || []).map((c) => (
          <span className="tag" key={c.id || c.name} title={c.source_name || c.name}>
            {c.name} <em>{ROLE_LABELS[c.role] || c.type}</em>
          </span>
        ))}
      </div>

      <ErrorBanner error={error} />

      {panel?.kind === "preview" ? (
        <div style={{ marginTop: 14 }}>
          <Table result={panel.data} limit={25} theme={theme} />
        </div>
      ) : null}

      {panel?.kind === "profile" ? (
        <div style={{ marginTop: 14 }}>
          <ProfileBlock profile={panel.data} theme={theme} />
        </div>
      ) : null}
    </div>
  );
}
