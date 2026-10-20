-- Каталог датасетов, диалоги и дашборды.
--
-- Миграции владеют только фиксированными таблицами в public. Схему сырых
-- данных (RAW_SCHEMA) создаёт приложение при старте: её имя — настройка
-- запуска, а не версия схемы.

CREATE TABLE IF NOT EXISTS datasets (
    id          uuid        PRIMARY KEY,
    name        text        NOT NULL UNIQUE,
    table_name  text        NOT NULL UNIQUE,
    source_file text        NOT NULL DEFAULT '',
    delimiter   text        NOT NULL DEFAULT ',',
    row_count   bigint      NOT NULL DEFAULT 0,
    size_bytes  bigint      NOT NULL DEFAULT 0,
    status      text        NOT NULL CHECK (status IN ('pending', 'ready', 'failed')),
    fail_reason text        NOT NULL DEFAULT '',
    description text        NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS dataset_columns (
    id          uuid        PRIMARY KEY,
    dataset_id  uuid        NOT NULL REFERENCES datasets (id) ON DELETE CASCADE,
    position    integer     NOT NULL,
    source_name text        NOT NULL,
    name        text        NOT NULL,
    data_type   text        NOT NULL,
    role        text        NOT NULL DEFAULT 'unknown',
    nullable    boolean     NOT NULL DEFAULT true,
    comment     text        NOT NULL DEFAULT '',
    profile     jsonb,
    UNIQUE (dataset_id, name),
    UNIQUE (dataset_id, position)
);

CREATE INDEX IF NOT EXISTS dataset_columns_dataset_idx ON dataset_columns (dataset_id, position);

CREATE TABLE IF NOT EXISTS dataset_relations (
    left_dataset_id  uuid             NOT NULL REFERENCES datasets (id) ON DELETE CASCADE,
    left_column      text             NOT NULL,
    right_dataset_id uuid             NOT NULL REFERENCES datasets (id) ON DELETE CASCADE,
    right_column     text             NOT NULL,
    overlap          double precision NOT NULL DEFAULT 0,
    matched_values   bigint           NOT NULL DEFAULT 0,
    cardinality      text             NOT NULL DEFAULT 'N:M',
    confidence       double precision NOT NULL DEFAULT 0,
    created_at       timestamptz      NOT NULL DEFAULT now(),
    PRIMARY KEY (left_dataset_id, left_column, right_dataset_id, right_column)
);

CREATE TABLE IF NOT EXISTS chat_sessions (
    id          uuid        PRIMARY KEY,
    title       text        NOT NULL DEFAULT 'Новый диалог',
    dataset_ids uuid[]      NOT NULL DEFAULT '{}',
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS chat_sessions_updated_idx ON chat_sessions (updated_at DESC);

CREATE TABLE IF NOT EXISTS chat_messages (
    id         uuid        PRIMARY KEY,
    session_id uuid        NOT NULL REFERENCES chat_sessions (id) ON DELETE CASCADE,
    role       text        NOT NULL CHECK (role IN ('user', 'assistant', 'system')),
    content    text        NOT NULL DEFAULT '',
    answer     jsonb,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS chat_messages_session_idx ON chat_messages (session_id, created_at);

CREATE TABLE IF NOT EXISTS dashboards (
    id          uuid        PRIMARY KEY,
    session_id  uuid        REFERENCES chat_sessions (id) ON DELETE SET NULL,
    title       text        NOT NULL,
    description text        NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS dashboards_created_idx ON dashboards (created_at DESC);

CREATE TABLE IF NOT EXISTS dashboard_widgets (
    id           uuid    PRIMARY KEY,
    dashboard_id uuid    NOT NULL REFERENCES dashboards (id) ON DELETE CASCADE,
    position     integer NOT NULL,
    title        text    NOT NULL DEFAULT '',
    question     text    NOT NULL DEFAULT '',
    sql          text    NOT NULL,
    chart        jsonb   NOT NULL DEFAULT '{}'::jsonb,
    width        integer NOT NULL DEFAULT 6 CHECK (width BETWEEN 1 AND 12),
    UNIQUE (dashboard_id, position)
);

CREATE INDEX IF NOT EXISTS dashboard_widgets_dashboard_idx ON dashboard_widgets (dashboard_id, position);
