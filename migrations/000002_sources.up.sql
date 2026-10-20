-- Прямое подключение к внешним базам данных.
--
-- Датасет перестаёт быть синонимом «загруженный CSV»: теперь у него есть
-- происхождение. Загруженные файлы лежат в собственной схеме платформы,
-- подключённые таблицы — во внешнем источнике и только читаются.

CREATE TABLE IF NOT EXISTS sources (
    id          uuid        PRIMARY KEY,
    name        text        NOT NULL UNIQUE,
    kind        text        NOT NULL CHECK (kind IN ('postgres')),
    dsn         text        NOT NULL,
    status      text        NOT NULL CHECK (status IN ('ready', 'failed')),
    fail_reason text        NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE datasets
    ADD COLUMN IF NOT EXISTS origin      text NOT NULL DEFAULT 'uploaded',
    ADD COLUMN IF NOT EXISTS source_id   uuid REFERENCES sources (id) ON DELETE CASCADE,
    ADD COLUMN IF NOT EXISTS schema_name text NOT NULL DEFAULT '';

ALTER TABLE datasets
    ADD CONSTRAINT datasets_origin_check CHECK (origin IN ('uploaded', 'attached'));

-- Ранее имя физической таблицы было уникальным глобально. С внешними
-- источниками одно и то же имя может встретиться в разных базах, поэтому
-- уникальна тройка «источник + схема + таблица».
ALTER TABLE datasets DROP CONSTRAINT IF EXISTS datasets_table_name_key;

CREATE UNIQUE INDEX IF NOT EXISTS datasets_physical_idx ON datasets (
    COALESCE(source_id, '00000000-0000-0000-0000-000000000000'::uuid),
    schema_name,
    table_name
);

CREATE INDEX IF NOT EXISTS datasets_source_idx ON datasets (source_id);
