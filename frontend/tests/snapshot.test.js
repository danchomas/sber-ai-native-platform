import { describe, expect, it } from "vitest";

import { imageFileName } from "../src/shared/lib/snapshot.js";

describe("имя файла картинки", () => {
  it("сохраняет русское название и убирает запрещённые символы", () => {
    expect(imageFileName("Продажи: кварталы/округа")).toBe("Продажи_ кварталы_округа.png");
  });

  it("подставляет имя по умолчанию", () => {
    expect(imageFileName("")).toBe("dashboard.png");
  });
});
