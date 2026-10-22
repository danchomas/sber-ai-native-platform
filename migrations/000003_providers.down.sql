-- Откат провайдеров. Платформа возвращается к модели из переменных
-- окружения — она и так остаётся значением по умолчанию.

DROP INDEX IF EXISTS llm_providers_active_idx;
DROP TABLE IF EXISTS llm_providers;
