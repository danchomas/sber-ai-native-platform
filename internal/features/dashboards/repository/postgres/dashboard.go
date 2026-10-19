package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/heinousness/aibi/internal/core/apperr"
	"github.com/heinousness/aibi/internal/core/domain"
	corepg "github.com/heinousness/aibi/internal/core/repository/postgres"
)

type DashboardRepository struct {
	pool *pgxpool.Pool
}

func NewDashboardRepository(pool *pgxpool.Pool) *DashboardRepository {
	return &DashboardRepository{pool: pool}
}

var _ domain.DashboardRepository = (*DashboardRepository)(nil)

func (r *DashboardRepository) Create(ctx context.Context, d *domain.Dashboard) error {
	return corepg.WithTx(ctx, r.pool, func(tx pgx.Tx) error {
		const q = `
			INSERT INTO dashboards (id, session_id, title, description)
			VALUES ($1, $2, $3, $4)
			RETURNING created_at, updated_at`
		if err := tx.QueryRow(ctx, q, d.ID, d.SessionID, d.Title, d.Description).
			Scan(&d.CreatedAt, &d.UpdatedAt); err != nil {
			return fmt.Errorf("создание дашборда: %w", corepg.MapError(err))
		}

		batch := &pgx.Batch{}
		for i := range d.Widgets {
			w := &d.Widgets[i]
			if w.ID == uuid.Nil {
				w.ID = uuid.New()
			}
			w.DashboardID = d.ID
			w.Position = i
			if w.Width <= 0 || w.Width > 12 {
				w.Width = 6
			}
			chart, err := json.Marshal(w.Chart)
			if err != nil {
				return fmt.Errorf("сериализация визуализации: %w", err)
			}
			batch.Queue(`
				INSERT INTO dashboard_widgets (id, dashboard_id, position, title, question, sql, chart, width)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
				w.ID, w.DashboardID, w.Position, w.Title, w.Question, w.SQL, chart, w.Width)
		}
		if batch.Len() == 0 {
			return nil
		}
		return corepg.MapError(tx.SendBatch(ctx, batch).Close())
	})
}

func (r *DashboardRepository) Get(ctx context.Context, id uuid.UUID) (*domain.Dashboard, error) {
	const q = `
		SELECT id, session_id, title, description, created_at, updated_at
		  FROM dashboards WHERE id = $1`
	var d domain.Dashboard
	err := r.pool.QueryRow(ctx, q, id).
		Scan(&d.ID, &d.SessionID, &d.Title, &d.Description, &d.CreatedAt, &d.UpdatedAt)
	if err != nil {
		return nil, corepg.MapError(err)
	}

	widgets, err := r.listWidgets(ctx, id)
	if err != nil {
		return nil, err
	}
	d.Widgets = widgets
	return &d, nil
}

func (r *DashboardRepository) List(ctx context.Context, limit int) ([]domain.Dashboard, error) {
	if limit <= 0 {
		limit = 50
	}
	const q = `
		SELECT id, session_id, title, description, created_at, updated_at
		  FROM dashboards ORDER BY created_at DESC LIMIT $1`
	rows, err := r.pool.Query(ctx, q, limit)
	if err != nil {
		return nil, fmt.Errorf("список дашбордов: %w", corepg.MapError(err))
	}
	defer rows.Close()

	var out []domain.Dashboard
	for rows.Next() {
		var d domain.Dashboard
		if err := rows.Scan(&d.ID, &d.SessionID, &d.Title, &d.Description,
			&d.CreatedAt, &d.UpdatedAt); err != nil {
			return nil, corepg.MapError(err)
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, corepg.MapError(err)
	}

	for i := range out {
		widgets, err := r.listWidgets(ctx, out[i].ID)
		if err != nil {
			return nil, err
		}
		out[i].Widgets = widgets
	}
	return out, nil
}

func (r *DashboardRepository) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM dashboards WHERE id = $1`, id)
	if err != nil {
		return corepg.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return apperr.ErrNotFound
	}
	return nil
}

func (r *DashboardRepository) listWidgets(ctx context.Context, dashboardID uuid.UUID) ([]domain.Widget, error) {
	const q = `
		SELECT id, dashboard_id, position, title, question, sql, chart, width
		  FROM dashboard_widgets WHERE dashboard_id = $1 ORDER BY position`
	rows, err := r.pool.Query(ctx, q, dashboardID)
	if err != nil {
		return nil, fmt.Errorf("виджеты дашборда: %w", corepg.MapError(err))
	}
	defer rows.Close()

	var out []domain.Widget
	for rows.Next() {
		var (
			w     domain.Widget
			chart []byte
		)
		if err := rows.Scan(&w.ID, &w.DashboardID, &w.Position, &w.Title,
			&w.Question, &w.SQL, &chart, &w.Width); err != nil {
			return nil, corepg.MapError(err)
		}
		if len(chart) > 0 {
			if err := json.Unmarshal(chart, &w.Chart); err != nil {
				return nil, fmt.Errorf("разбор визуализации виджета: %w", err)
			}
		}
		out = append(out, w)
	}
	return out, corepg.MapError(rows.Err())
}
