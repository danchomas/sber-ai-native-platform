DELETE FROM dashboard_widgets WHERE deleted_at IS NOT NULL;
DELETE FROM dashboards WHERE deleted_at IS NOT NULL;

DROP INDEX IF EXISTS dashboard_widgets_deleted_idx;
DROP INDEX IF EXISTS dashboards_deleted_idx;

ALTER TABLE dashboard_widgets DROP COLUMN IF EXISTS deleted_at;
ALTER TABLE dashboards DROP COLUMN IF EXISTS deleted_at;
