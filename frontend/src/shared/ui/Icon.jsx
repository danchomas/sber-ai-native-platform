// Иконки интерфейса — собственные линейные глифы, встроенные в сборку.
//
// Почему не внешний набор: платформа не ходит в сеть за ресурсами
// (требование автономности стенда), а бесплатные лицензии популярных
// сервисов требуют видимой ссылки на поставщика в интерфейсе.
//
// Все глифы нарисованы в одной системе: сетка 24, обводка 1.75,
// скруглённые концы, без заливок. Размер и цвет наследуются от текста.

const PATHS = {
  // Диалог — реплика с хвостом.
  chat: <path d="M4 6.5A2.5 2.5 0 0 1 6.5 4h11A2.5 2.5 0 0 1 20 6.5v7a2.5 2.5 0 0 1-2.5 2.5H10l-4 3.5v-3.5a2 2 0 0 1-2-2z" />,

  // Данные — стопка таблиц.
  data: (
    <>
      <ellipse cx="12" cy="6" rx="7" ry="2.6" />
      <path d="M5 6v6c0 1.4 3.1 2.6 7 2.6s7-1.2 7-2.6V6" />
      <path d="M5 12v5c0 1.4 3.1 2.6 7 2.6s7-1.2 7-2.6v-5" />
    </>
  ),

  // Источники — разъём.
  sources: (
    <>
      <path d="M9 3v4M15 3v4" />
      <path d="M6.5 7h11v4a5.5 5.5 0 0 1-11 0z" />
      <path d="M12 16.5V21" />
    </>
  ),

  // Дашборды — столбцы.
  dashboards: (
    <>
      <path d="M4 20h16" />
      <path d="M7 20v-6M12 20V7M17 20v-9" />
    </>
  ),

  // Тема — круг, наполовину закрашенный обводкой.
  theme: (
    <>
      <circle cx="12" cy="12" r="8" />
      <path d="M12 4a8 8 0 0 0 0 16z" fill="currentColor" stroke="none" />
    </>
  ),

  sparkles: <><path d="m12 3 2.4 6.6L21 12l-6.6 2.4L12 21l-2.4-6.6L3 12l6.6-2.4z" /><path d="m20 2 .6 1.4L22 4l-1.4.6L20 6l-.6-1.4L18 4l1.4-.6z" /></>,
  arrow: <path d="M7 17 17 7M7 7h10v10" />,
  send: <path d="m6 11 6-6 6 6M12 5v14" />,
  check: <path d="m5 12 4 4L19 6" />,
  chevron: <path d="m9 5 7 7-7 7" />,
  plus: <path d="M12 5v14M5 12h14" />,
  close: <path d="M6 6l12 12M18 6L6 18" />,
  refresh: (
    <>
      <path d="M20 12a8 8 0 1 1-2.6-5.9" />
      <path d="M20 4v5h-5" />
    </>
  ),
  // Щит — сработала защита ассистента.
  shield: (
    <>
      <path d="M12 3 5 6v5.5c0 4.3 3 8 7 9.5 4-1.5 7-5.2 7-9.5V6z" />
      <path d="m9 12 2.2 2.2L15.5 10" />
    </>
  ),
  // Принтер — печать и сохранение в PDF.
  print: (
    <>
      <path d="M7 9V4h10v5" />
      <path d="M7 17H5a2 2 0 0 1-2-2v-4a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2v4a2 2 0 0 1-2 2h-2" />
      <path d="M7 14h10v6H7z" />
    </>
  ),
  // Карандаш — переименовать.
  edit: <path d="M4 20h4L19 9l-4-4L4 16zM14 6l4 4" />,
};

export function Icon({ name, size = 18, className, ...rest }) {
  const glyph = PATHS[name];
  if (!glyph) return null;

  return (
    <svg
      className={className}
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.75"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      focusable="false"
      {...rest}
    >
      {glyph}
    </svg>
  );
}
