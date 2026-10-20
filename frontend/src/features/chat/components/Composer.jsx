import { useRef, useState } from "react";

// Composer: Enter отправляет, Shift+Enter переносит строку.
// Поле растёт под текст — вопросы бывают длинными.
export function Composer({ busy, onAsk }) {
  const [value, setValue] = useState("");
  const area = useRef(null);

  const grow = () => {
    const node = area.current;
    if (!node) return;
    node.style.height = "auto";
    node.style.height = `${Math.min(node.scrollHeight, 200)}px`;
  };

  const send = () => {
    const text = value.trim();
    if (!text || busy) return;
    setValue("");
    if (area.current) area.current.style.height = "auto";
    onAsk(text);
  };

  return (
    <div className="composer">
      <div className="composer-inner">
        <div className="composer-box">
          <textarea
            ref={area}
            rows={1}
            value={value}
            placeholder="Например: что есть в этих данных? Покажи динамику по месяцам."
            onChange={(e) => {
              setValue(e.target.value);
              grow();
            }}
            onKeyDown={(e) => {
              if (e.key === "Enter" && !e.shiftKey) {
                e.preventDefault();
                send();
              }
            }}
          />
          <button className="send" onClick={send} disabled={busy}>
            {busy ? "…" : "Спросить"}
          </button>
        </div>
        <p className="hint">
          Enter — отправить, Shift+Enter — перенос строки. Контекст диалога учитывается.
        </p>
      </div>
    </div>
  );
}
