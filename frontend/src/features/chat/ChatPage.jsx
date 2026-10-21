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
    if (node) node.scrollTop = node.scrollHeight;
  }, [messages, busy]);

  return (
    <section className={`view${active ? " active" : ""}`}>
      <div className="view-head">
        <h2>{sessions.active?.title || "Новый диалог"}</h2>
        <p>Спросите о загруженных данных на обычном языке</p>
      </div>

      <div className="scroll" ref={scroll}>
        <div className={`thread col${messages.length === 0 && !busy ? " thread-empty" : ""}`}>
          {error ? <ErrorBanner error={error} /> : null}

          {messages.length === 0 && !busy ? <Welcome onAsk={ask} /> : null}

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

      <Composer busy={busy} onAsk={ask} />
    </section>
  );
}
