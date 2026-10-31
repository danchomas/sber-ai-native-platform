ALTER TABLE dashboards ADD COLUMN IF NOT EXISTS deleted_at timestamptz;
ALTER TABLE dashboard_widgets ADD COLUMN IF NOT EXISTS deleted_at timestamptz;

CREATE INDEX IF NOT EXISTS dashboards_deleted_idx ON dashboards (deleted_at) WHERE deleted_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS dashboard_widgets_deleted_idx ON dashboard_widgets (deleted_at) WHERE deleted_at IS NOT NULL;
