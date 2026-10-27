import { useCallback, useEffect, useState } from "react";

import { api } from "../../../shared/api/PlatformApi.js";

// useChat держит историю активного диалога и отправляет вопросы.
//
// Пока ответ готовится, в ленту кладётся заглушка с ходом работы: агент
// может думать десятки секунд, и шаги приходят потоком по мере выполнения.
export function useChat(sessionId, { onAnswered } = {}) {
  const [messages, setMessages] = useState([]);
  const [busy, setBusy] = useState(false);
  const [steps, setSteps] = useState([]);
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
      setSteps([]);
      setBusy(true);
      setMessages((prev) => [...prev, { id: `local-${Date.now()}`, role: "user", content: question }]);

      try {
        const data = await api.askStream(sessionId, question, (step) => setSteps((prev) => [...prev, step]));
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

  return { messages, busy, steps, error, ask };
}
