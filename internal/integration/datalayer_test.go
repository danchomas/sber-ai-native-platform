//go:build integration

package integration

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/heinousness/aibi/internal/core/config"
	"github.com/heinousness/aibi/internal/core/domain"
	corepg "github.com/heinousness/aibi/internal/core/repository/postgres"
	pgpool "github.com/heinousness/aibi/internal/core/repository/postgres/pool/pgx"
	"github.com/heinousness/aibi/internal/core/sqlguard"
	chatpg "github.com/heinousness/aibi/internal/features/chat/repository/postgres"
	dashpg "github.com/heinousness/aibi/internal/features/dashboards/repository/postgres"
	dashsvc "github.com/heinousness/aibi/internal/features/dashboards/service"
	datapg "github.com/heinousness/aibi/internal/features/datasets/repository/postgres"
	datasvc "github.com/heinousness/aibi/internal/features/datasets/service"
	srcpg "github.com/heinousness/aibi/internal/features/sources/repository/postgres"
)

const testSchema = "raw_itest"

var (
	ingestCfg = config.Ingest{
		MaxUploadBytes:   64 << 20,
		CopyBatchRows:    1000,
		TypeSampleRows:   500,
		ProfileTopValues: 5,
	}
	queryCfg = config.Query{MaxRows: 1000, Timeout: 30 * time.Second, PreviewRows: 20, MaxRepairs: 1, HistoryDepth: 8}
)

const ordersCSV = `Номер заказа;Код клиента;Дата заказа;Статус;Сумма, руб.;Канал
1001;0042;01.03.2024;оплачен;1 250,50;сайт
1002;0043;02.03.2024;новый;980,00;мобильное приложение
1003;0042;15.03.2024;оплачен;15 400,75;сайт
1004;0044;20.04.2024;отменён;;колл-центр
1005;0043;05.05.2024;оплачен;2 300,10;сайт
1005;0043;05.05.2024;оплачен;2 300,10;сайт
`

const clientsCSV = `Код клиента,Сегмент,Город
0042,премиум,Москва
0043,массовый,Казань
0044,массовый,Новосибирск
0099,премиум,Сочи
`

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func setup(t *testing.T) *pgxpool.Pool {
	t.Helper()

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL не задан — интеграционные тесты пропущены")
	}

	ctx := context.Background()
	pool, err := pgpool.NewPool(ctx, pgpool.PoolOptions{
		DSN: dsn, MaxConns: 8, MinConns: 1,
		ConnectTimeout: 10 * time.Second, StatementTimeout: 30 * time.Second,
		AppName: "aibi-itest",
	})
	if err != nil {
		t.Fatalf("подключение к тестовой базе: %v", err)
	}

	cleanup := func() {
		_, _ = pool.Exec(ctx, `DROP SCHEMA IF EXISTS `+`"`+testSchema+`"`+` CASCADE`)
		_, _ = pool.Exec(ctx, `DROP TABLE IF EXISTS dashboard_widgets, dashboards, chat_messages,
			chat_sessions, dataset_relations, dataset_columns, datasets, schema_migrations CASCADE`)
	}
	cleanup()

	if _, err := corepg.Migrate(ctx, pool, testSchema); err != nil {
		pool.Close()
		t.Fatalf("миграции: %v", err)
	}
	t.Cleanup(func() {
		cleanup()
		pool.Close()
	})
	return pool
}

type env struct {
	datasetRepo *datapg.DatasetRepository
	relations   *datapg.RelationRepository
	raw         *corepg.RawDataRepository
	sessions    *chatpg.SessionRepository
	dashes      *dashpg.DashboardRepository
	datasets    *datasvc.Service
}

func newEnv(t *testing.T, pool *pgxpool.Pool) *env {
	t.Helper()
	log := testLogger()

	datasets := datapg.NewDatasetRepository(pool)
	relations := datapg.NewRelationRepository(pool)
	registry := pgpool.NewRegistry(pool, srcpg.NewSourceRepository(pool),
		pgpool.PoolOptions{DSN: os.Getenv("TEST_DATABASE_URL")})
	raw := corepg.NewRawDataRepository(registry, testSchema, queryCfg.Timeout)

	return &env{
		datasetRepo: datasets,
		relations:   relations,
		raw:         raw,
		sessions:    chatpg.NewSessionRepository(pool),
		dashes:      dashpg.NewDashboardRepository(pool),
		datasets:    datasvc.New(datasets, relations, raw, testSchema, ingestCfg, queryCfg, log),
	}
}

func upload(t *testing.T, e *env, name, body string) *datasvc.Report {
	t.Helper()
	report, err := e.datasets.Upload(context.Background(), datasvc.Request{
		Name:     name,
		Filename: name + ".csv",
		Reader:   strings.NewReader(body),
	})
	if err != nil {
		t.Fatalf("загрузка %s: %v", name, err)
	}
	return report
}

func TestIngestRawCSV(t *testing.T) {
	pool := setup(t)
	e := newEnv(t, pool)
	ctx := context.Background()

	report := upload(t, e, "orders", ordersCSV)

	if report.RowsLoaded != 6 {
		t.Fatalf("загружено %d строк, ожидалось 6", report.RowsLoaded)
	}
	if report.Dialect != ";" {
		t.Fatalf("разделитель определён как %q", report.Dialect)
	}

	dataset := report.Dataset
	if !dataset.Ready() {
		t.Fatalf("статус датасета: %s (%s)", dataset.Status, dataset.FailReason)
	}

	wantTypes := map[string]domain.ColumnType{
		"nomer_zakaza": domain.ColumnTypeInteger,
		"kod_klienta":  domain.ColumnTypeText,
		"data_zakaza":  domain.ColumnTypeDate,
		"status":       domain.ColumnTypeText,
		"summa_rub":    domain.ColumnTypeNumeric,
		"kanal":        domain.ColumnTypeText,
	}
	for name, want := range wantTypes {
		col, ok := dataset.Column(name)
		if !ok {
			t.Fatalf("колонка %s не найдена, есть: %+v", name, columnNames(dataset.Columns))
		}
		if col.Type != want {
			t.Errorf("колонка %s: тип %s, ожидался %s", name, col.Type, want)
		}
	}

	wantRoles := map[string]domain.SemanticRole{
		"kod_klienta": domain.RoleIdentifier,
		"data_zakaza": domain.RoleTemporal,
		"status":      domain.RoleDimension,
		"summa_rub":   domain.RoleMeasure,
	}
	for name, want := range wantRoles {
		col, _ := dataset.Column(name)
		if col.Role != want {
			t.Errorf("колонка %s: роль %s, ожидалась %s", name, col.Role, want)
		}
	}

	amount, _ := dataset.Column("summa_rub")
	if amount.Profile == nil {
		t.Fatal("профиль суммы не посчитан")
	}
	if amount.Profile.NullCount != 1 {
		t.Errorf("пропусков в сумме: %d, ожидался 1", amount.Profile.NullCount)
	}
	if amount.Profile.MaxValue == nil || !strings.HasPrefix(*amount.Profile.MaxValue, "15400") {
		t.Errorf("максимум суммы: %v", amount.Profile.MaxValue)
	}

	profile, err := e.datasets.Profile(ctx, dataset.ID)
	if err != nil {
		t.Fatalf("профиль датасета: %v", err)
	}
	if profile.DuplicateRows != 1 {
		t.Errorf("дубликатов строк: %d, ожидался 1", profile.DuplicateRows)
	}
	if len(profile.TimeCoverage) != 1 || profile.TimeCoverage[0].Days < 60 {
		t.Errorf("временной охват: %+v", profile.TimeCoverage)
	}
	if profile.Sample == nil || len(profile.Sample.Rows) == 0 {
		t.Error("пример строк не отдан")
	}
}

func TestDiscoverRelations(t *testing.T) {
	pool := setup(t)
	e := newEnv(t, pool)
	ctx := context.Background()

	upload(t, e, "orders", ordersCSV)
	upload(t, e, "clients", clientsCSV)

	relations, err := e.relations.List(ctx)
	if err != nil {
		t.Fatalf("список связей: %v", err)
	}
	if len(relations) == 0 {
		t.Fatal("связь по коду клиента не выведена")
	}

	found := false
	for _, r := range relations {
		if r.LeftColumn == "kod_klienta" && r.RightColumn == "kod_klienta" {
			found = true
			if r.Overlap < 0.9 {
				t.Errorf("пересечение %.2f, ожидалось почти полное", r.Overlap)
			}
			if r.Cardinality != "N:1" {
				t.Errorf("тип связи %s, ожидался N:1", r.Cardinality)
			}
		}
	}
	if !found {
		t.Fatalf("ожидаемая связь не найдена: %+v", relations)
	}
}

func TestGuardedQueryExecution(t *testing.T) {
	pool := setup(t)
	e := newEnv(t, pool)
	ctx := context.Background()

	report := upload(t, e, "orders", ordersCSV)
	scopes, err := e.datasets.Scopes(ctx)
	if err != nil {
		t.Fatalf("области каталога: %v", err)
	}
	if len(scopes) == 0 {
		t.Fatal("каталог не отдал ни одной области")
	}
	target := scopes[0].Target
	policy := sqlguard.PolicyFor(scopes[0], testSchema, queryCfg.MaxRows)

	t.Run("агрегация выполняется", func(t *testing.T) {
		raw := `SELECT status, count(*) AS "Количество", sum(summa_rub) AS "Сумма"
		          FROM ` + report.Dataset.TableName + `
		         GROUP BY status ORDER BY 2 DESC`
		checked, err := sqlguard.Validate(raw, policy)
		if err != nil {
			t.Fatalf("проверка запроса: %v", err)
		}

		result, err := e.raw.Query(ctx, target, checked.SQL, queryCfg.MaxRows)
		if err != nil {
			t.Fatalf("выполнение запроса: %v", err)
		}
		if result.RowCount != 3 {
			t.Fatalf("получено %d групп, ожидалось 3", result.RowCount)
		}
		if result.Columns[1].Type != domain.ColumnTypeInteger {
			t.Errorf("тип количества: %s", result.Columns[1].Type)
		}
	})

	t.Run("изменение данных невозможно", func(t *testing.T) {
		_, err := e.raw.Query(ctx, target, `DELETE FROM `+report.Dataset.TableName, 10)
		if err == nil {
			t.Fatal("удаление данных прошло через read-only транзакцию")
		}
	})

	t.Run("поиск по значению колонки", func(t *testing.T) {
		checked, err := sqlguard.Validate(
			`SELECT count(*) FROM `+report.Dataset.TableName+` WHERE kod_klienta = '0042'`, policy)
		if err != nil {
			t.Fatalf("проверка запроса: %v", err)
		}
		result, err := e.raw.Query(ctx, target, checked.SQL, 10)
		if err != nil {
			t.Fatalf("выполнение: %v", err)
		}
		if got := result.Rows[0][0]; got != int64(2) {
			t.Fatalf("найдено %v заказов, ожидалось 2", got)
		}
	})
}

func TestSessionHistory(t *testing.T) {
	pool := setup(t)
	e := newEnv(t, pool)
	ctx := context.Background()

	session := &domain.Session{ID: uuid.New(), Title: "Новый диалог"}
	if err := e.sessions.CreateSession(ctx, session); err != nil {
		t.Fatalf("создание сессии: %v", err)
	}

	question := &domain.Message{ID: uuid.New(), SessionID: session.ID, Role: domain.RoleUser, Content: "сколько заказов?"}
	if err := e.sessions.AddMessage(ctx, question); err != nil {
		t.Fatalf("вопрос: %v", err)
	}

	answer := &domain.Message{
		ID: uuid.New(), SessionID: session.ID, Role: domain.RoleAssistant,
		Content: "Всего 6 заказов.",
		Answer: &domain.Answer{
			Intent: domain.IntentQuery,
			Text:   "Всего 6 заказов.",
			SQL:    "SELECT count(*) FROM orders",
			Chart:  &domain.ChartSpec{Type: domain.ChartTypeKPI, Y: []string{"count"}},
		},
	}
	if err := e.sessions.AddMessage(ctx, answer); err != nil {
		t.Fatalf("ответ: %v", err)
	}

	messages, err := e.sessions.ListMessages(ctx, session.ID, 10)
	if err != nil {
		t.Fatalf("история: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("сообщений: %d", len(messages))
	}
	if messages[1].Answer == nil || messages[1].Answer.Chart == nil {
		t.Fatal("структурированный ответ не сохранён")
	}

	turns, err := e.sessions.LastTurns(ctx, session.ID, 5)
	if err != nil {
		t.Fatalf("реплики: %v", err)
	}
	if len(turns) != 1 || turns[0].Question != "сколько заказов?" || turns[0].SQL == "" {
		t.Fatalf("контекст диалога собран неверно: %+v", turns)
	}
}

func TestDashboardRoundTrip(t *testing.T) {
	pool := setup(t)
	e := newEnv(t, pool)
	ctx := context.Background()

	report := upload(t, e, "orders", ordersCSV)
	svc := dashsvc.New(e.dashes, e.datasets, e.raw, queryCfg, testLogger())

	created, err := svc.CreateFromWidgets(ctx, nil, "Сводка по заказам", "тест", []domain.Widget{
		{
			Title: "Заказы по статусам",
			SQL:   `SELECT status, count(*) AS "Количество" FROM ` + report.Dataset.TableName + ` GROUP BY status`,
			Chart: domain.ChartSpec{Type: domain.ChartTypeBar, X: "status", Y: []string{"Количество"}},
			Width: 6,
		},
		{
			Title: "Запрос к несуществующей таблице",
			SQL:   `SELECT * FROM no_such_table`,
			Chart: domain.ChartSpec{Type: domain.ChartTypeTable},
			Width: 6,
		},
	})
	if err != nil {
		t.Fatalf("создание дашборда: %v", err)
	}

	loaded, err := svc.Get(ctx, created.ID, true)
	if err != nil {
		t.Fatalf("чтение дашборда: %v", err)
	}
	if len(loaded.Widgets) != 2 {
		t.Fatalf("виджетов: %d", len(loaded.Widgets))
	}
	if loaded.Widgets[0].Result == nil || loaded.Widgets[0].Result.RowCount != 3 {
		t.Fatalf("данные первого виджета не пересчитаны: %+v", loaded.Widgets[0].Result)
	}
	if loaded.Widgets[0].Chart.Type != domain.ChartTypeBar {
		t.Errorf("спецификация визуализации не сохранена: %+v", loaded.Widgets[0].Chart)
	}
	if loaded.Widgets[1].Error == "" {
		t.Error("ошибка второго виджета не зафиксирована")
	}

	if err := svc.Delete(ctx, created.ID); err != nil {
		t.Fatalf("удаление дашборда: %v", err)
	}
	if _, err := svc.Get(ctx, created.ID, false); err == nil {
		t.Fatal("удалённый дашборд всё ещё читается")
	}
}

func TestDatasetDeletionDropsTable(t *testing.T) {
	pool := setup(t)
	e := newEnv(t, pool)
	ctx := context.Background()

	report := upload(t, e, "orders", ordersCSV)
	if err := e.datasets.Delete(ctx, report.Dataset.ID); err != nil {
		t.Fatalf("удаление датасета: %v", err)
	}

	var exists bool
	err := pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM pg_tables WHERE schemaname = $1 AND tablename = $2)`,
		testSchema, report.Dataset.TableName).Scan(&exists)
	if err != nil {
		t.Fatalf("проверка таблицы: %v", err)
	}
	if exists {
		t.Fatal("физическая таблица осталась после удаления датасета")
	}
}

func columnNames(cols []domain.Column) []string {
	out := make([]string, 0, len(cols))
	for _, c := range cols {
		out = append(out, c.Name)
	}
	return out
}
