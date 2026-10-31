package service

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/heinousness/aibi/internal/core/domain"
)

type DashboardRepository interface {
	Create(ctx context.Context, d *domain.Dashboard) error
	Get(ctx context.Context, id uuid.UUID) (*domain.Dashboard, error)
	List(ctx context.Context, limit int) ([]domain.Dashboard, error)
	Delete(ctx context.Context, id uuid.UUID) error

	AddWidget(ctx context.Context, dashboardID uuid.UUID, w *domain.Widget) error
	DeleteWidget(ctx context.Context, dashboardID, widgetID uuid.UUID) error
	Rename(ctx context.Context, id uuid.UUID, title string) error

	Trash(ctx context.Context, retention time.Duration) ([]domain.TrashItem, error)
	RestoreDashboard(ctx context.Context, id uuid.UUID, retention time.Duration) error
	RestoreWidget(ctx context.Context, dashboardID, widgetID uuid.UUID, retention time.Duration) error
	PurgeDeleted(ctx context.Context, retention time.Duration) (int64, error)
}
