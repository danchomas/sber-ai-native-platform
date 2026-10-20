import { useCallback, useEffect, useState } from "react";

import { api } from "../../../shared/api/PlatformApi.js";

// useChat держит историю активного диалога и отправляет вопросы.
//
// Пока ответ готовится, в ленту кладётся заглушка: агент может думать
// десятки секунд — он планирует SQL, выполняет его и переписывает при ошибке.
export function useChat(sessionId, { onAnswered } = {}) {
  const [messages, setMessages] = useState([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState(null);

  useEffect(() => {
    if (!sessionId) return;
    let alive = true;
    api
      .messages(sessionId)
      .then((m) => alive && setMessages(m))
      .catch((err) => alive && setError(err));
    return () => {
      alive = false;
    };
  }, [sessionId]);

  const ask = useCallback(
    async (text) => {
      const question = (text || "").trim();
      if (!question || busy || !sessionId) return;

      setError(null);
      setBusy(true);
      setMessages((prev) => [...prev, { id: `local-${Date.now()}`, role: "user", content: question }]);

      try {
        const data = await api.ask(sessionId, question);
        setMessages((prev) => [
          ...prev,
          { id: `answer-${Date.now()}`, role: "assistant", content: data.answer?.text, answer: data.answer },
        ]);
        onAnswered?.(data.answer);
      } catch (err) {
        setError(err);
      } finally {
        setBusy(false);
      }
    },
    [busy, sessionId, onAnswered],
  );

  return { messages, busy, error, ask };
}
