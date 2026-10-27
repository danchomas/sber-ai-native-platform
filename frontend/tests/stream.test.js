// Поток событий ответа агента: шаги, затем ответ; ошибка — исключение.

import { describe, expect, it, vi } from "vitest";

import { HttpClient, parseEvent } from "../src/shared/api/HttpClient.js";

function streamResponse(chunks) {
  const encoder = new TextEncoder();
  return new Response(
    new ReadableStream({
      start(controller) {
        for (const c of chunks) controller.enqueue(encoder.encode(c));
        controller.close();
      },
    }),
    { status: 200, headers: { "Content-Type": "text/event-stream" } },
  );
}

describe("поток ответа", () => {
  it("разбирает событие", () => {
    expect(parseEvent('event: step\ndata: {"title":"Посчитал"}')).toEqual({ name: "step", data: { title: "Посчитал" } });
    expect(parseEvent("event: ping")).toBeNull();
  });

  it("отдаёт шаги по мере прихода и возвращает ответ, даже если событие разрезано", async () => {
    globalThis.fetch = vi.fn().mockResolvedValue(
      streamResponse([
        'event: step\ndata: {"title":"Изучил"}\n\n',
        'event: step\ndata: {"tit',
        'le":"Посчитал"}\n\nevent: answer\ndata: {"answer":{"text":"Готово."}}\n\n',
      ]),
    );
    const steps = [];
    const result = await new HttpClient().stream("/x", {}, (name, data) => name === "step" && steps.push(data.title));
    expect(steps).toEqual(["Изучил", "Посчитал"]);
    expect(result.answer.text).toBe("Готово.");
  });

  it("событие ошибки становится исключением с текстом для пользователя", async () => {
    globalThis.fetch = vi.fn().mockResolvedValue(
      streamResponse(['event: error\ndata: {"code":"busy","message":"Ассистент занят"}\n\n']),
    );
    await expect(new HttpClient().stream("/x", {})).rejects.toMatchObject({ code: "busy", message: "Ассистент занят" });
  });
});
