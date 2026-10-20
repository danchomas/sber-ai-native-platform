-- Откат начальной схемы.
--
-- Порядок обратный созданию: сначала таблицы со ссылками, потом те,
-- на которые ссылаются. Индексы уходят вместе со своими таблицами.

DROP TABLE IF EXISTS dashboard_widgets;
DROP TABLE IF EXISTS dashboards;
DROP TABLE IF EXISTS chat_messages;
DROP TABLE IF EXISTS chat_sessions;
DROP TABLE IF EXISTS dataset_relations;
DROP TABLE IF EXISTS dataset_columns;
DROP TABLE IF EXISTS datasets;
