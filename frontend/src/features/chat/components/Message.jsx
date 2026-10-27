import { useState } from "react";

import { PinDialog } from "./PinDialog.jsx";
import { Chart } from "../../../shared/charts/Chart.jsx";
import { plural } from "../../../shared/lib/plural.js";
import { Headline } from "../../../shared/ui/Headline.jsx";
import { Icon } from "../../../shared/ui/Icon.jsx";

const INTENT_LABELS = {
  overview: "описание данных",
  query: "расчёт по данным",
  dashboard: "дашборд",
  clarify: "уточнение",
  reject: "отказ",
  smalltalk: "о платформе",
};

const ANALYSIS_LABELS = {
  what_if: "сценарий «что если»",
  forecast: "прогноз",
  seasonality: "сезонность",
  correlation: "связь показателей",
  anomalies: "выбросы",
};

const CHART_LABELS = {
  kpi: "Число",
  bar: "Столбцы",
  hbar: "Полосы",
  line: "Линия",
  area: "Область",
  pie: "Круг",
  donut: "Кольцо",
  scatter: "Точки",
  histogram: "Распределение",
  heatmap: "Тепловая карта",
  table: "Таблица",
};

export function UserMessage({ text }) {
  return (
    <div className="msg user">
      <div className="who">Я</div>
      <div className="body">
        <div className="text">{text}</div>
      </div>
    </div>
  );
}

export function PendingMessage({ steps = [] }) {
  return (
    <div className="msg">
      <div className="who">AI</div>
      <div className="body">
        <div className="card">
          <div className="row">
            <span className="spinner" />
            <span className="muted" aria-live="polite">
              {steps.length ? `${steps[steps.length - 1].title}…` : "изучаю вопрос…"}
            </span>
          </div>
          {steps.length ? (
            <ol className="live-steps">
              {steps.map((s, i) => (
                <li key={i} className={s.ok ? "ok" : "fail"}>
                  <span className="step-mark" aria-hidden="true">{s.ok ? "✓" : "↻"}</span>
                  <span className="step-title">{s.title}</span>
                  {s.detail ? <span className="step-detail">{s.detail}</span> : null}
                </li>
              ))}
            </ol>
          ) : null}
        </div>
      </div>
    </div>
  );
}

export function AssistantMessage({ answer, fallbackText, theme, onAsk, onOpenDashboard }) {
  const hasResult = answer?.result?.columns?.length > 0;
  const options = answer?.chart_options || [];
  const [chart, setChart] = useState(answer?.chart);
  const [pinning, setPinning] = useState(false);
  const canPin = hasResult && !answer.analysis && answer.query && answer.intent === "query";
  const meta = [];
  if (answer) {
    meta.push(ANALYSIS_LABELS[answer.analysis] || INTENT_LABELS[answer.intent] || answer.intent);
    if (answer.datasets?.length) meta.push(answer.datasets.join(", "));
    if (answer.elapsed_ms) meta.push(`${(answer.elapsed_ms / 1000).toFixed(1)} с`);
  }

  return (
    <div className="msg">
      <div className="who">AI</div>
      <div className="body">
        <div className="card">
          <div className="text">{answer?.text || fallbackText || ""}</div>

          {answer ? (
            <div className="answer-extras">
              {(answer.security || []).map((note, i) => (
                <div className="notice security" key={`s${i}`} role="status">
                  <Icon name="shield" size={16} />
                  <span>{note}</span>
                </div>
              ))}

              {(answer.warnings || []).map((w, i) => (
                <div className="notice" key={i}>
                  <span>⚠</span>
                  <span>{w}</span>
                </div>
              ))}

              {hasResult && chart?.type !== "kpi" ? <Headline headline={answer.headline} compact /> : null}

              {hasResult ? <Chart spec={chart} result={answer.result} height={300} theme={theme} /> : null}

              {hasResult && (options.length > 1 || canPin) ? (
                <div className="answer-tools">
                  {options.length > 1 ? (
                    <div className="chips view-switch" role="group" aria-label="Вид графика">
                      {options.map((o) => (
                        <button
                          key={o.type}
                          className={`chip${chart?.type === o.type ? " active" : ""}`}
                          aria-pressed={chart?.type === o.type}
                          onClick={() => setChart(o)}
                        >
                          {CHART_LABELS[o.type] || o.type}
                        </button>
                      ))}
                    </div>
                  ) : null}
                  {canPin ? (
                    <button className="icon-btn" onClick={() => setPinning(true)}>
                      Закрепить на дашборде
                    </button>
                  ) : null}
                </div>
              ) : null}

              {answer.dashboard_id ? (
                <div className="chips">
                  <button className="chip" onClick={() => onOpenDashboard(answer.dashboard_id)}>
                    Открыть дашборд →
                  </button>
                </div>
              ) : null}

              {(answer.followups || []).length ? (
                <div className="chips">
                  {answer.followups.map((text) => (
                    <button key={text} className="chip" onClick={() => onAsk(text)}>
                      {text}
                    </button>
                  ))}
                </div>
              ) : null}

              {answer.steps?.length ? <AgentSteps steps={answer.steps} /> : null}

              {answer.sql ? (
                <details className="sql">
                  <summary>Как это посчитано (SQL)</summary>
                  <pre>{answer.sql}</pre>
                </details>
              ) : null}
            </div>
          ) : null}
        </div>

        {meta.length ? <div className="meta">{meta.join(" · ")}</div> : null}
      </div>

      {pinning ? (
        <PinDialog
          widget={{
            title: chart?.title || answer.text?.slice(0, 60) || "Показатель",
            query: answer.query,
            chart,
            width: chart?.type === "kpi" ? 3 : chart?.type === "line" || chart?.type === "area" ? 12 : 6,
          }}
          onClose={() => setPinning(false)}
          onPinned={onOpenDashboard}
        />
      ) : null}
    </div>
  );
}

function AgentSteps({ steps }) {
  const failed = steps.filter((s) => !s.ok).length;
  return (
    <details className="sql steps">
      <summary>
        {`Ход работы: ${steps.length} ${plural(steps.length, "шаг", "шага", "шагов")}` +
          (failed ? ` · исправлено ошибок: ${failed}` : "")}
      </summary>
      <ol>
        {steps.map((s, i) => (
          <li key={i} className={s.ok ? "ok" : "fail"}>
            <span className="step-mark" aria-hidden="true">{s.ok ? "✓" : "↻"}</span>
            <span className="step-title">{s.title}</span>
            <span className="step-meta">
              {s.ok && s.rows ? `${s.rows} ${plural(s.rows, "строка", "строки", "строк")} · ` : ""}
              {(s.elapsed_ms / 1000).toFixed(1)} с
            </span>
            {s.detail ? <div className="step-detail">{s.detail}</div> : null}
          </li>
        ))}
      </ol>
    </details>
  );
}
