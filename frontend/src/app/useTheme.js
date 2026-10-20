import { useCallback, useEffect, useState } from "react";

const KEY = "aibi-theme";

function readStored() {
  try {
    return localStorage.getItem(KEY);
  } catch {
    // Приватный режим: обходимся предпочтением системы.
    return null;
  }
}

// useTheme хранит тему в атрибуте документа — от него зависят CSS-токены,
// из которых движок графиков читает цвета.
export function useTheme() {
  const [theme, setTheme] = useState(() => {
    const stored = readStored();
    if (stored) return stored;
    const prefersLight = window.matchMedia?.("(prefers-color-scheme: light)").matches;
    return prefersLight ? "light" : "dark";
  });

  useEffect(() => {
    document.documentElement.dataset.theme = theme;
    try {
      localStorage.setItem(KEY, theme);
    } catch {
      // Не критично: тема переживёт только текущую вкладку.
    }
  }, [theme]);

  const toggle = useCallback(() => {
    setTheme((t) => (t === "dark" ? "light" : "dark"));
  }, []);

  return { theme, toggle };
}
