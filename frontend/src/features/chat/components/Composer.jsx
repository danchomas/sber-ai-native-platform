import { useRef, useState } from "react";
import { Icon } from "../../../shared/ui/Icon.jsx";

// Composer: Enter отправляет, Shift+Enter переносит строку.
// Поле растёт под текст — вопросы бывают длинными.
export function Composer({ busy, disabled, onAsk }) {
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
    if (!text || busy || disabled) return;
    setValue("");
    if (area.current) area.current.style.height = "auto";
    onAsk(text);
  };

  return (
    <div className="composer">
      <div className="composer-inner">
        <div className="composer-box">
          <textarea
            aria-label="Вопрос к AI-аналитику"
            aria-describedby="composer-hint"
            disabled={disabled}
            ref={area}
            rows={1}
            value={value}
            placeholder="Что вы хотите узнать о своих данных?"
            onChange={(e) => {
              setValue(e.target.value);
              grow();
            }}
            onKeyDown={(e) => {
              if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) {
                e.preventDefault();
                send();
              }
            }}
          />
          <button className="send" onClick={send} disabled={busy || disabled || !value.trim()} aria-label={busy ? "Готовится ответ" : "Отправить вопрос"} title="Отправить вопрос">
            {busy ? <span className="spinner" /> : <Icon name="send" size={21} />}
          </button>
        </div>
        <p className="hint" id="composer-hint">
          <span>AI может ошибаться. Проверяйте важные выводы.</span><span>Enter ↵ <span className="keyboard-hint">· Shift+Enter — новая строка</span></span>
        </p>
      </div>
    </div>
  );
}
