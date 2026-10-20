-- Откат внешних источников.
--
-- Датасеты, пришедшие из источников, исчезают вместе с ними: платформа
-- не владеет их данными, поэтому удаляются только метаданные каталога.
-- Сами таблицы во внешних базах не затрагиваются.

DELETE FROM datasets WHERE source_id IS NOT NULL;

DROP INDEX IF EXISTS datasets_source_idx;
DROP INDEX IF EXISTS datasets_physical_idx;

ALTER TABLE datasets DROP CONSTRAINT IF EXISTS datasets_origin_check;

ALTER TABLE datasets
    DROP COLUMN IF EXISTS schema_name,
    DROP COLUMN IF EXISTS source_id,
    DROP COLUMN IF EXISTS origin;

-- Возвращаем прежнюю глобальную уникальность имени таблицы.
ALTER TABLE datasets ADD CONSTRAINT datasets_table_name_key UNIQUE (table_name);

DROP TABLE IF EXISTS sources;
