-- Провайдеры языковых моделей.
--
-- Платформа и раньше умела работать с любым OpenAI-совместимым сервером,
-- но адрес задавался переменными окружения и менялся только перезапуском.
-- Эта таблица позволяет держать несколько провайдеров и переключаться
-- между ними на ходу: локальная модель остаётся, облачная добавляется.

CREATE TABLE IF NOT EXISTS llm_providers (
    id          uuid        PRIMARY KEY,
    name        text        NOT NULL UNIQUE,
    kind        text        NOT NULL CHECK (kind IN ('openai_compatible', 'openrouter')),
    base_url    text        NOT NULL,
    api_key     text        NOT NULL DEFAULT '',
    model       text        NOT NULL,
    is_active   boolean     NOT NULL DEFAULT false,
    status      text        NOT NULL CHECK (status IN ('ready', 'failed')),
    fail_reason text        NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

-- Активный провайдер ровно один: частичный уникальный индекс не даёт
-- включить второй, не выключив первый.
CREATE UNIQUE INDEX IF NOT EXISTS llm_providers_active_idx
    ON llm_providers (is_active) WHERE is_active;
