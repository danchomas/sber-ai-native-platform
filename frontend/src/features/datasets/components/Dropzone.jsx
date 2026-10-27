import { useRef, useState } from "react";

import { fmtNum } from "../../../shared/lib/format.js";

const ACCEPT = ".csv,.tsv,.txt";

export function Dropzone({ onUpload }) {
  const input = useRef(null);
  const [over, setOver] = useState(false);
  const [status, setStatus] = useState(null);
  const [busy, setBusy] = useState(false);

  const send = async (file) => {
    if (!file || busy) return;
    setBusy(true);
    setStatus({ tone: "muted", text: `загружаю «${file.name}»…` });
    try {
      const report = await onUpload(file);
      setStatus({
        tone: "ok",
        text: `«${report.dataset.name}»: ${fmtNum(report.rows_loaded)} строк за ${(report.elapsed_ms / 1000).toFixed(1)} с`,
      });
    } catch (err) {
      setStatus({ tone: "error", text: err.message });
    } finally {
      setBusy(false);
    }
  };

  return (
    <div
      className={`dropzone${over ? " over" : ""}`}
      onDragOver={(e) => {
        e.preventDefault();
        setOver(true);
      }}
      onDragLeave={() => setOver(false)}
      onDrop={(e) => {
        e.preventDefault();
        setOver(false);
        send(e.dataTransfer.files?.[0]);
      }}
    >
      <h3>Перетащите CSV-файл сюда</h3>
      <p>или выберите файл — разделитель, кодировка и типы колонок определяются автоматически</p>
      <input
        ref={input}
        type="file"
        accept={ACCEPT}
        className="sr-only"
        onChange={(e) => send(e.target.files?.[0])}
      />
      <button className="send" disabled={busy} onClick={() => input.current?.click()}>
        {busy ? "Загружаю…" : "Выбрать файл"}
      </button>
      {status ? (
        <div
          className={status.tone === "error" ? "notice error" : "muted"}
          style={{ marginTop: 12 }}
        >
          {status.text}
        </div>
      ) : null}
    </div>
  );
}
