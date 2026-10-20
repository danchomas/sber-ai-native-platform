// Связи выводятся статистически, по пересечению идентификаторов: внешних
// ключей в CSV нет, а в подключённых таблицах платформа их не читает.
export function RelationList({ relations }) {
  if (!relations.length) return null;

  return (
    <div className="card">
      <h3>Связи между наборами</h3>
      <p className="muted" style={{ margin: "6px 0 12px" }}>
        Выведены по пересечению значений — внешних ключей в данных нет.
        Соединять можно только таблицы одной базы.
      </p>
      {relations.map((r, i) => (
        <div className="muted" key={i}>
          «{r.left_dataset}».{r.left_column} → «{r.right_dataset}».{r.right_column} —
          пересечение {Math.round(r.overlap * 100)}%, тип {r.cardinality}
        </div>
      ))}
    </div>
  );
}
