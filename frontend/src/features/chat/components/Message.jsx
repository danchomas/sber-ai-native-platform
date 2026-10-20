import { Chart } from "../../../shared/charts/Chart.jsx";

const INTENT_LABELS = {
  overview: "описание данных",
  query: "расчёт по данным",
  dashboard: "дашборд",
  clarify: "уточнение",
  reject: "отказ",
  smalltalk: "о платформе",
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

export function PendingMessage() {
  return (
    <div className="msg">
      <div className="who">AI</div>
      <div className="body">
        <div className="card row">
          <span className="spinner" />
          <span className="muted">думаю над вопросом…</span>
        </div>
      </div>
    </div>
  );
}

// AssistantMessage показывает ответ агента целиком: текст, предупреждения,
// визуализацию, ссылку на дашборд, продолжения и SQL под катом.
export function AssistantMessage({ answer, fallbackText, theme, onAsk, onOpenDashboard }) {
  const hasResult = answer?.result?.columns?.length > 0;
  const meta = [];
  if (answer) {
    meta.push(INTENT_LABELS[answer.intent] || answer.intent);
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
              {(answer.warnings || []).map((w, i) => (
                <div className="notice" key={i}>
                  <span>⚠</span>
                  <span>{w}</span>
                </div>
              ))}

              {hasResult ? (
                <Chart spec={answer.chart} result={answer.result} height={300} theme={theme} />
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
    </div>
  );
}
