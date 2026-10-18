package sqlguard

import (
	"fmt"
	"strings"

	"github.com/heinousness/aibi/internal/core/apperr"
	"github.com/heinousness/aibi/internal/core/domain"
)

var forbiddenKeywords = map[string]string{
	"INSERT": "изменение данных", "UPDATE": "изменение данных",
	"DELETE": "удаление данных", "TRUNCATE": "удаление данных",
	"MERGE": "изменение данных", "UPSERT": "изменение данных",
	"DROP": "изменение схемы", "ALTER": "изменение схемы",
	"CREATE": "изменение схемы", "COMMENT": "изменение схемы",
	"REFRESH": "изменение схемы", "REINDEX": "обслуживание БД",
	"VACUUM": "обслуживание БД", "ANALYZE": "обслуживание БД",
	"CLUSTER": "обслуживание БД", "LOCK": "блокировка таблиц",
	"GRANT": "управление доступом", "REVOKE": "управление доступом",
	"SET": "изменение параметров сессии", "RESET": "изменение параметров сессии",
	"DISCARD": "изменение параметров сессии", "LOAD": "загрузка модулей",
	"COPY": "ввод-вывод файлов", "IMPORT": "ввод-вывод файлов",
	"CALL": "вызов процедуры", "DO": "выполнение кода",
	"EXECUTE": "динамический SQL", "PREPARE": "динамический SQL",
	"DEALLOCATE": "динамический SQL", "DECLARE": "курсоры",
	"FETCH": "курсоры", "MOVE": "курсоры", "CLOSE": "курсоры",
	"LISTEN": "асинхронные уведомления", "NOTIFY": "асинхронные уведомления",
	"UNLISTEN": "асинхронные уведомления",
	"BEGIN":    "управление транзакцией", "COMMIT": "управление транзакцией",
	"ROLLBACK": "управление транзакцией", "SAVEPOINT": "управление транзакцией",
	"INTO": "создание таблиц через SELECT INTO",
}

var forbiddenFunctions = map[string]struct{}{
	"pg_read_file": {}, "pg_read_binary_file": {}, "pg_ls_dir": {},
	"pg_stat_file": {}, "pg_ls_logdir": {}, "pg_ls_waldir": {},
	"lo_import": {}, "lo_export": {}, "lo_get": {}, "lo_put": {},
	"dblink": {}, "dblink_connect": {}, "dblink_exec": {}, "postgres_fdw_handler": {},
	"pg_sleep": {}, "pg_sleep_for": {}, "pg_sleep_until": {},
	"pg_terminate_backend": {}, "pg_cancel_backend": {}, "pg_reload_conf": {},
	"pg_rotate_logfile": {}, "set_config": {}, "current_setting": {},
	"pg_logical_emit_message": {}, "query_to_xml": {}, "xmltable": {},
	"pg_backup_start": {}, "pg_backup_stop": {}, "pg_switch_wal": {},
}

var forbiddenSchemas = map[string]struct{}{
	"pg_catalog": {}, "information_schema": {}, "pg_toast": {}, "pg_temp": {},
}

type Policy struct {
	Schemas       map[string]struct{}
	DefaultSchema string
	AllowedTables map[string]struct{}
	MaxRows       int
	MaxLength     int
	MaxJoins      int
}

func NewPolicy(schema string, tables []string, maxRows int) Policy {
	qualified := make([]TableName, 0, len(tables))
	for _, t := range tables {
		qualified = append(qualified, TableName{Schema: schema, Table: t})
	}
	return NewSourcePolicy(schema, qualified, maxRows)
}

type TableName struct {
	Schema string
	Table  string
}

func (t TableName) String() string {
	return strings.ToLower(t.Schema) + "." + strings.ToLower(t.Table)
}

func NewSourcePolicy(defaultSchema string, tables []TableName, maxRows int) Policy {
	allowed := make(map[string]struct{}, len(tables))
	schemas := make(map[string]struct{}, len(tables)+1)
	for _, t := range tables {
		allowed[t.String()] = struct{}{}
		schemas[strings.ToLower(t.Schema)] = struct{}{}
	}
	if defaultSchema != "" {
		schemas[strings.ToLower(defaultSchema)] = struct{}{}
	}
	return Policy{
		Schemas:       schemas,
		DefaultSchema: defaultSchema,
		AllowedTables: allowed,
		MaxRows:       maxRows,
		MaxLength:     20_000,
		MaxJoins:      10,
	}
}

func (p Policy) SchemaList() []string {
	out := make([]string, 0, len(p.Schemas))
	if p.DefaultSchema != "" {
		out = append(out, p.DefaultSchema)
	}
	for s := range p.Schemas {
		if !strings.EqualFold(s, p.DefaultSchema) {
			out = append(out, s)
		}
	}
	return out
}

type Result struct {
	SQL      string
	Tables   []string
	Warnings []string
	Wrapped  bool
}

func Validate(raw string, p Policy) (Result, error) {
	sql := strings.TrimSpace(raw)
	sql = strings.TrimSuffix(sql, ";")
	sql = strings.TrimSpace(sql)

	if sql == "" {
		return Result{}, reject("пустой запрос")
	}
	if p.MaxLength > 0 && len(sql) > p.MaxLength {
		return Result{}, reject(fmt.Sprintf("запрос длиннее %d символов", p.MaxLength))
	}

	tokens, err := tokenize(sql)
	if err != nil {
		return Result{}, reject(err.Error())
	}
	if len(tokens) == 0 {
		return Result{}, reject("запрос не содержит выражений")
	}

	if !tokens[0].isWord("SELECT") && !tokens[0].isWord("WITH") && !tokens[0].isWord("TABLE") {
		return Result{}, reject("разрешены только запросы SELECT/WITH")
	}

	if err := checkStatements(tokens); err != nil {
		return Result{}, err
	}
	if err := checkKeywords(tokens); err != nil {
		return Result{}, err
	}
	if err := checkFunctions(tokens); err != nil {
		return Result{}, err
	}

	res := Result{}
	tables, warnings, err := checkRelations(tokens, p)
	if err != nil {
		return Result{}, err
	}
	res.Tables = tables
	res.Warnings = warnings

	if joins := countJoins(tokens); p.MaxJoins > 0 && joins > p.MaxJoins {
		return Result{}, reject(fmt.Sprintf("слишком много соединений (%d, предел %d)", joins, p.MaxJoins))
	}

	res.SQL = sql
	if p.MaxRows > 0 && !hasTopLevelLimit(tokens) {
		res.SQL = fmt.Sprintf("SELECT * FROM (\n%s\n) AS aibi_guarded LIMIT %d", sql, p.MaxRows)
		res.Wrapped = true
		res.Warnings = append(res.Warnings,
			fmt.Sprintf("Результат ограничен %d строками.", p.MaxRows))
	}
	return res, nil
}

func checkStatements(tokens []token) error {
	for _, t := range tokens {
		if t.isPunct(";") {
			return reject("в одном запросе допускается только одна инструкция")
		}
	}
	return nil
}

func checkKeywords(tokens []token) error {
	for i, t := range tokens {
		if t.kind != tokWord {
			continue
		}
		up := t.upper()
		reason, forbidden := forbiddenKeywords[up]
		if !forbidden {
			continue
		}
		if up == "ANALYZE" && i > 0 && tokens[i-1].isWord("EXPLAIN") {
			return reject("EXPLAIN ANALYZE выполняет запрос и запрещён")
		}
		return reject(fmt.Sprintf("ключевое слово %s запрещено (%s)", up, reason))
	}
	return nil
}

func checkFunctions(tokens []token) error {
	for i, t := range tokens {
		if t.kind != tokWord || i+1 >= len(tokens) || !tokens[i+1].isPunct("(") {
			continue
		}
		if _, bad := forbiddenFunctions[strings.ToLower(t.text)]; bad {
			return reject(fmt.Sprintf("функция %s() запрещена", t.text))
		}
	}
	return nil
}

func checkRelations(tokens []token, p Policy) ([]string, []string, error) {
	ctes := collectCTENames(tokens)
	seen := map[string]struct{}{}
	var (
		tables   []string
		warnings []string
	)

	for i := 0; i < len(tokens); i++ {
		t := tokens[i]
		if t.kind != tokWord {
			continue
		}
		switch t.upper() {
		case "FROM", "JOIN":
		default:
			continue
		}
		if i+1 >= len(tokens) {
			continue
		}
		next := tokens[i+1]
		if next.isPunct("(") {
			continue
		}
		if next.kind != tokWord && next.kind != tokQuotedIdent {
			continue
		}
		if i+2 < len(tokens) && tokens[i+2].isPunct("(") {
			continue
		}

		parts, consumed := qualifiedName(tokens, i+1)
		i += consumed

		schema, table := "", strings.ToLower(parts[len(parts)-1])
		if len(parts) > 1 {
			schema = strings.ToLower(parts[len(parts)-2])
		}

		if schema == "" {
			if _, ok := ctes[table]; ok {
				continue
			}
		}
		if _, bad := forbiddenSchemas[schema]; bad {
			return nil, nil, reject(fmt.Sprintf("доступ к системной схеме %s запрещён", schema))
		}
		if strings.HasPrefix(table, "pg_") {
			return nil, nil, reject(fmt.Sprintf("доступ к системному объекту %s запрещён", table))
		}
		if schema == "" {
			schema = strings.ToLower(p.DefaultSchema)
		}
		if _, ok := p.Schemas[schema]; !ok {
			return nil, nil, reject(fmt.Sprintf("схема %s недоступна по этому вопросу", schema))
		}
		qualified := schema + "." + table
		if _, ok := p.AllowedTables[qualified]; !ok {
			return nil, nil, reject(fmt.Sprintf("таблица %q отсутствует в каталоге данных", qualified))
		}
		if _, dup := seen[qualified]; !dup {
			seen[qualified] = struct{}{}
			tables = append(tables, qualified)
		}
	}

	if len(tables) == 0 {
		warnings = append(warnings, "Запрос не обращается к загруженным данным.")
	}
	return tables, warnings, nil
}

func qualifiedName(tokens []token, start int) (parts []string, consumed int) {
	parts = []string{tokens[start].text}
	i := start + 1
	for i+1 < len(tokens) && tokens[i].isPunct(".") &&
		(tokens[i+1].kind == tokWord || tokens[i+1].kind == tokQuotedIdent) {
		parts = append(parts, tokens[i+1].text)
		i += 2
	}
	return parts, i - start
}

func collectCTENames(tokens []token) map[string]struct{} {
	out := map[string]struct{}{}
	for i := 0; i+2 < len(tokens); i++ {
		if tokens[i].kind != tokWord && tokens[i].kind != tokQuotedIdent {
			continue
		}
		if !tokens[i+1].isWord("AS") {
			continue
		}
		j := i + 2
		for j < len(tokens) && (tokens[j].isWord("MATERIALIZED") || tokens[j].isWord("NOT")) {
			j++
		}
		if j < len(tokens) && tokens[j].isPunct("(") {
			out[strings.ToLower(tokens[i].text)] = struct{}{}
		}
	}
	return out
}

func hasTopLevelLimit(tokens []token) bool {
	depth := 0
	for _, t := range tokens {
		switch {
		case t.isPunct("("):
			depth++
		case t.isPunct(")"):
			depth--
		case depth == 0 && t.kind == tokWord && (t.upper() == "LIMIT" || t.upper() == "FETCH"):
			return true
		}
	}
	return false
}

func countJoins(tokens []token) int {
	n := 0
	for _, t := range tokens {
		if t.kind == tokWord && t.upper() == "JOIN" {
			n++
		}
	}
	return n
}

func reject(reason string) error {
	return fmt.Errorf("%w: %s", apperr.ErrUnsafeQuery, reason)
}

func PolicyFor(sc domain.Scope, defaultSchema string, maxRows int) Policy {
	tables := make([]TableName, 0, len(sc.Datasets))
	for _, d := range sc.Datasets {
		tables = append(tables, TableName{Schema: d.Schema, Table: d.TableName})
	}
	if !sc.Local() && len(sc.Target.Schemas) > 0 {
		defaultSchema = sc.Target.Schemas[0]
	}
	return NewSourcePolicy(defaultSchema, tables, maxRows)
}

func ResolveScope(sql string, scopes []domain.Scope, defaultSchema string, maxRows int) (domain.Scope, Result, error) {
	var firstErr error
	for _, sc := range scopes {
		checked, err := Validate(sql, PolicyFor(sc, defaultSchema, maxRows))
		if err == nil {
			return sc, checked, nil
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	if firstErr == nil {
		firstErr = reject("нет доступных данных для запроса")
	}
	if len(scopes) > 1 {
		firstErr = fmt.Errorf("%w (таблицы разных источников нельзя соединить одним запросом)", firstErr)
	}
	return domain.Scope{}, Result{}, firstErr
}
