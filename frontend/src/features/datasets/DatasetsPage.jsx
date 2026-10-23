import { DatasetCard } from "./components/DatasetCard.jsx";
import { Dropzone } from "./components/Dropzone.jsx";
import { RelationList } from "./components/RelationList.jsx";
import { EmptyState } from "../../shared/ui/EmptyState.jsx";
import { ErrorBanner } from "../../shared/ui/ErrorBanner.jsx";
import { Spinner } from "../../shared/ui/Spinner.jsx";
import "./styles.css";

export function DatasetsPage({ active, theme, datasets }) {
  return (
    <section className={`view${active ? " active" : ""}`}>
      <div className="view-head">
        <h2>Данные</h2>
        <p>Загрузите файлы — AI-аналитик поможет разобраться в цифрах</p>
      </div>

      <div className="scroll">
        <div className="grid col-wide">
          <Dropzone onUpload={datasets.upload} />

          <ErrorBanner error={datasets.error} onRetry={datasets.reload} />

          {datasets.loading && !datasets.items.length ? <Spinner label="читаю каталог…" /> : null}

          {!datasets.loading && !datasets.items.length ? (
            <EmptyState title="Данных пока нет">
              Загрузите CSV или подключите существующую базу на вкладке «Источники» —
              платформа сама разберёт структуру и посчитает профиль.
            </EmptyState>
          ) : null}

          <div className="grid">
            {datasets.items.map((d) => (
              <DatasetCard key={d.id} dataset={d} theme={theme} onDelete={datasets.remove} />
            ))}
          </div>

          <RelationList relations={datasets.relations} />
        </div>
      </div>
    </section>
  );
}
