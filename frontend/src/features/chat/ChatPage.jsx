import { useEffect, useRef } from "react";

import { Composer } from "./components/Composer.jsx";
import { AssistantMessage, PendingMessage, UserMessage } from "./components/Message.jsx";
import { Welcome } from "./components/Welcome.jsx";
import { useChat } from "./hooks/useChat.js";
import { ErrorBanner } from "../../shared/ui/ErrorBanner.jsx";
import "./styles.css";

export function ChatPage({ active, theme, sessions, onOpenDashboard }) {
  const { messages, busy, error, ask } = useChat(sessions.activeId, {
    onAnswered: (answer) => {
      sessions.reload();
      if (answer?.dashboard_id) onOpenDashboard(answer.dashboard_id);
    },
  });
  const scroll = useRef(null);

  useEffect(() => {
    const node = scroll.current;
    if (node) node.scrollTop = messages.length || busy ? node.scrollHeight : 0;
  }, [messages, busy]);

  return (
    <section className={`view chat-view${active ? " active" : ""}${messages.length === 0 && !busy ? " is-empty" : ""}`} aria-label="Чат с аналитиком">
      <div className="view-head">
        <div className="chat-heading"><h2>{sessions.active?.title || "Новый диалог"}</h2><p>Рабочее пространство</p></div>
        <span className="assistant-badge"><span /> AI-аналитик</span>
      </div>

      <div className="scroll" ref={scroll}>
        <div className={`thread col${messages.length === 0 && !busy ? " thread-empty" : ""}`}>
          {error || sessions.error ? <ErrorBanner error={error || sessions.error} /> : null}

          {messages.length === 0 && !busy ? <Welcome onAsk={ask} disabled={!sessions.activeId} /> : null}

          {messages.map((m) =>
            m.role === "user" ? (
              <UserMessage key={m.id} text={m.content} />
            ) : (
              <AssistantMessage
                key={m.id}
                answer={m.answer}
                fallbackText={m.content}
                theme={theme}
                onAsk={ask}
                onOpenDashboard={onOpenDashboard}
              />
            ),
          )}

          {busy ? <PendingMessage /> : null}
        </div>
      </div>

      <Composer busy={busy} disabled={!sessions.activeId} onAsk={ask} />
    </section>
  );
}
