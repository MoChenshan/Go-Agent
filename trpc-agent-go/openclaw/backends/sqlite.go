package backends

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	agentlog "trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/memory"
	memextractor "trpc.group/trpc-go/trpc-agent-go/memory/extractor"
	memorytool "trpc.group/trpc-go/trpc-agent-go/memory/tool"
	_ "trpc.group/trpc-go/trpc-agent-go/openclaw/app"
	"trpc.group/trpc-go/trpc-agent-go/openclaw/registry"
	"trpc.group/trpc-go/trpc-agent-go/session"
	agenttool "trpc.group/trpc-go/trpc-agent-go/tool"
)

const (
	memoryBackendSQLite = "sqlite"

	sqliteDriverName = "sqlite3"

	sqliteSchemaInitTimeout = 5 * time.Second

	defaultSQLiteMemoryDSN = "file:memories.db?_busy_timeout=5000"
	defaultMemoryTableName = "openclaw_memories"
	memoryJSONColumnName   = "memory_json"

	defaultMemoryLimit = 1000

	sqliteConfigErrBadTable = "sqlite memory table_name must match %s"

	sqliteSchema = `CREATE TABLE IF NOT EXISTS %s (
app_name TEXT NOT NULL,
user_id TEXT NOT NULL,
memory_id TEXT NOT NULL,
memory_text TEXT NOT NULL,
topics_json TEXT NOT NULL,
memory_json TEXT,
created_at TEXT NOT NULL,
updated_at TEXT NOT NULL,
deleted_at TEXT,
PRIMARY KEY (app_name, user_id, memory_id)
)`

	sqliteIndexByUser = `CREATE INDEX IF NOT EXISTS %s
ON %s (app_name, user_id, updated_at DESC, created_at DESC)`
)

var sqliteTableNamePattern = regexp.MustCompile(
	`^[A-Za-z_][A-Za-z0-9_]*$`,
)

func init() {
	if _, ok := registry.LookupMemoryBackend(memoryBackendSQLite); ok {
		return
	}
	if err := registry.RegisterMemoryBackend(
		memoryBackendSQLite,
		newSQLiteMemoryBackend,
	); err != nil {
		panic(err)
	}
}

type sqliteMemoryConfig struct {
	DSN        string `yaml:"dsn,omitempty"`
	Path       string `yaml:"path,omitempty"`
	TableName  string `yaml:"table_name,omitempty"`
	SkipDBInit bool   `yaml:"skip_db_init,omitempty"`
	SoftDelete *bool  `yaml:"soft_delete,omitempty"`
}

type sqliteMemoryService struct {
	db          *sql.DB
	tableName   string
	softDelete  bool
	memoryLimit int

	cachedTools      map[string]agenttool.Tool
	precomputedTools []agenttool.Tool
	autoMemoryWorker *sqliteAutoMemoryWorker
}

func newSQLiteMemoryBackend(
	deps registry.MemoryDeps,
	spec registry.MemoryBackendSpec,
) (memory.Service, error) {
	if err := ensureSQLiteDriver(); err != nil {
		return nil, err
	}

	cfg := sqliteMemoryConfig{}
	if err := registry.DecodeStrict(spec.Config, &cfg); err != nil {
		return nil, err
	}

	dsn, path := resolveSQLiteDSN(
		strings.TrimSpace(cfg.DSN),
		strings.TrimSpace(cfg.Path),
	)
	if err := ensureSQLiteDir(path); err != nil {
		return nil, err
	}

	tableName := strings.TrimSpace(cfg.TableName)
	if tableName == "" {
		tableName = defaultMemoryTableName
	}
	if !sqliteTableNamePattern.MatchString(tableName) {
		return nil, fmt.Errorf(
			sqliteConfigErrBadTable,
			sqliteTableNamePattern.String(),
		)
	}

	db, err := sql.Open(sqliteDriverName, dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	if !cfg.SkipDBInit {
		if err := initSQLiteSchema(db, tableName); err != nil {
			_ = db.Close()
			return nil, err
		}
	}

	softDelete := false
	if cfg.SoftDelete != nil {
		softDelete = *cfg.SoftDelete
	}

	limit := spec.Limit
	if limit <= 0 {
		limit = defaultMemoryLimit
	}

	svc := &sqliteMemoryService{
		db:          db,
		tableName:   tableName,
		softDelete:  softDelete,
		memoryLimit: limit,
		cachedTools: make(map[string]agenttool.Tool),
	}

	enabledTools := defaultEnabledTools()
	if deps.Extractor != nil {
		enabledTools = autoModeEnabledTools()
		configureExtractorEnabledTools(deps.Extractor, enabledTools)
	}
	svc.precomputedTools = buildMemoryTools(
		deps.Extractor,
		enabledTools,
		svc.cachedTools,
	)

	if deps.Extractor != nil {
		svc.autoMemoryWorker = newSQLiteAutoMemoryWorker(
			sqliteAutoMemoryConfig{
				Extractor:    deps.Extractor,
				EnabledTools: enabledTools,
			},
			svc,
		)
		svc.autoMemoryWorker.Start()
	}

	return svc, nil
}

func resolveSQLiteDSN(dsn string, path string) (string, string) {
	if dsn == "" && path == "" {
		return defaultSQLiteMemoryDSN, ""
	}
	if dsn == "" {
		return path, path
	}
	return dsn, path
}

func ensureSQLiteDir(path string) error {
	path = strings.TrimSpace(path)
	if path == "" || path == ":memory:" {
		return nil
	}
	dir := filepath.Dir(path)
	if dir == "" || dir == "." {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir sqlite memory dir: %w", err)
	}
	return nil
}

func initSQLiteSchema(db *sql.DB, tableName string) error {
	ctx, cancel := context.WithTimeout(
		context.Background(),
		sqliteSchemaInitTimeout,
	)
	defer cancel()

	stmts := []string{
		fmt.Sprintf(sqliteSchema, tableName),
		fmt.Sprintf(
			sqliteIndexByUser,
			sqliteIndexName(tableName),
			tableName,
		),
	}
	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	if err := ensureSQLiteSchemaColumns(ctx, db, tableName); err != nil {
		return err
	}
	return nil
}

func sqliteIndexName(tableName string) string {
	return tableName + "_user_updated_idx"
}

func ensureSQLiteSchemaColumns(
	ctx context.Context,
	db *sql.DB,
	tableName string,
) error {
	query := fmt.Sprintf("PRAGMA table_info(%s)", tableName)
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return err
	}
	defer rows.Close()

	columns := make(map[string]struct{})
	for rows.Next() {
		var (
			cid      int
			name     string
			dataType string
			notNull  int
			defValue sql.NullString
			pk       int
		)
		if err := rows.Scan(
			&cid,
			&name,
			&dataType,
			&notNull,
			&defValue,
			&pk,
		); err != nil {
			return err
		}
		columns[name] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if _, ok := columns[memoryJSONColumnName]; ok {
		return nil
	}

	stmt := fmt.Sprintf(
		"ALTER TABLE %s ADD COLUMN %s TEXT",
		tableName,
		memoryJSONColumnName,
	)
	_, err = db.ExecContext(ctx, stmt)
	return err
}

func (s *sqliteMemoryService) AddMemory(
	ctx context.Context,
	userKey memory.UserKey,
	memoryText string,
	topics []string,
	opts ...memory.AddOption,
) error {
	if err := userKey.CheckUserKey(); err != nil {
		return err
	}

	now := time.Now().UTC()
	mem := newMemoryRecord(
		memoryText,
		topics,
		memory.ResolveAddOptions(opts),
		now,
	)
	entryID := generateMemoryID(mem, userKey.AppName, userKey.UserID)
	exists, err := s.memoryExists(ctx, userKey, entryID)
	if err != nil {
		return err
	}
	if !exists {
		count, err := s.countActiveMemories(ctx, userKey)
		if err != nil {
			return err
		}
		if count >= s.memoryLimit {
			return fmt.Errorf(
				"memory limit exceeded for user %s, limit: %d, current: %d",
				userKey.UserID,
				s.memoryLimit,
				count,
			)
		}
	}

	topicsJSON, err := marshalTopics(mem.Topics)
	if err != nil {
		return err
	}
	memoryJSON, err := marshalMemoryJSON(mem)
	if err != nil {
		return err
	}

	stmt := fmt.Sprintf(
		`INSERT INTO %s (
app_name, user_id, memory_id, memory_text, topics_json, memory_json,
created_at, updated_at, deleted_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, NULL)
ON CONFLICT(app_name, user_id, memory_id)
DO UPDATE SET
memory_text = excluded.memory_text,
topics_json = excluded.topics_json,
memory_json = excluded.memory_json,
updated_at = excluded.updated_at,
deleted_at = NULL`,
		s.tableName,
	)

	_, err = s.db.ExecContext(
		ctx,
		stmt,
		userKey.AppName,
		userKey.UserID,
		entryID,
		memoryText,
		topicsJSON,
		memoryJSON,
		now.Format(time.RFC3339Nano),
		now.Format(time.RFC3339Nano),
	)
	return err
}

func (s *sqliteMemoryService) UpdateMemory(
	ctx context.Context,
	memoryKey memory.Key,
	memoryText string,
	topics []string,
	opts ...memory.UpdateOption,
) error {
	if err := memoryKey.CheckMemoryKey(); err != nil {
		return err
	}

	entry, err := s.readMemoryEntry(ctx, memoryKey)
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	mem := cloneMemory(entry.Memory)
	mem.Memory = memoryText
	mem.Topics = dedupStrings(topics)
	mem.LastUpdated = &now
	applyMemoryMetadataPatch(mem, memory.ResolveUpdateOptions(opts))

	topicsJSON, err := marshalTopics(mem.Topics)
	if err != nil {
		return err
	}
	memoryJSON, err := marshalMemoryJSON(mem)
	if err != nil {
		return err
	}
	newID := generateMemoryID(mem, memoryKey.AppName, memoryKey.UserID)

	stmt := fmt.Sprintf(
		`UPDATE %s
SET memory_id = ?, memory_text = ?, topics_json = ?, memory_json = ?,
updated_at = ?, deleted_at = NULL
WHERE app_name = ? AND user_id = ? AND memory_id = ?
AND deleted_at IS NULL`,
		s.tableName,
	)
	res, err := s.db.ExecContext(
		ctx,
		stmt,
		newID,
		memoryText,
		topicsJSON,
		memoryJSON,
		now.Format(time.RFC3339Nano),
		memoryKey.AppName,
		memoryKey.UserID,
		memoryKey.MemoryID,
	)
	if err != nil {
		return err
	}
	if result := memory.ResolveUpdateResult(opts); result != nil {
		result.MemoryID = newID
	}
	return rowsAffectedOrNotFound(res, memoryKey.MemoryID)
}

func (s *sqliteMemoryService) DeleteMemory(
	ctx context.Context,
	memoryKey memory.Key,
) error {
	if err := memoryKey.CheckMemoryKey(); err != nil {
		return err
	}

	var (
		res sql.Result
		err error
	)
	if s.softDelete {
		stmt := fmt.Sprintf(
			`UPDATE %s
SET deleted_at = ?
WHERE app_name = ? AND user_id = ? AND memory_id = ?
AND deleted_at IS NULL`,
			s.tableName,
		)
		res, err = s.db.ExecContext(
			ctx,
			stmt,
			time.Now().UTC().Format(time.RFC3339Nano),
			memoryKey.AppName,
			memoryKey.UserID,
			memoryKey.MemoryID,
		)
	} else {
		stmt := fmt.Sprintf(
			`DELETE FROM %s
WHERE app_name = ? AND user_id = ? AND memory_id = ?
AND deleted_at IS NULL`,
			s.tableName,
		)
		res, err = s.db.ExecContext(
			ctx,
			stmt,
			memoryKey.AppName,
			memoryKey.UserID,
			memoryKey.MemoryID,
		)
	}
	if err != nil {
		return err
	}
	return rowsAffectedOrNotFound(res, memoryKey.MemoryID)
}

func (s *sqliteMemoryService) ClearMemories(
	ctx context.Context,
	userKey memory.UserKey,
) error {
	if err := userKey.CheckUserKey(); err != nil {
		return err
	}

	var (
		stmt string
		args []any
	)
	if s.softDelete {
		stmt = fmt.Sprintf(
			`UPDATE %s
SET deleted_at = ?
WHERE app_name = ? AND user_id = ? AND deleted_at IS NULL`,
			s.tableName,
		)
		args = []any{
			time.Now().UTC().Format(time.RFC3339Nano),
			userKey.AppName,
			userKey.UserID,
		}
	} else {
		stmt = fmt.Sprintf(
			`DELETE FROM %s
WHERE app_name = ? AND user_id = ? AND deleted_at IS NULL`,
			s.tableName,
		)
		args = []any{userKey.AppName, userKey.UserID}
	}
	_, err := s.db.ExecContext(ctx, stmt, args...)
	return err
}

func (s *sqliteMemoryService) ReadMemories(
	ctx context.Context,
	userKey memory.UserKey,
	limit int,
) ([]*memory.Entry, error) {
	if err := userKey.CheckUserKey(); err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(
		ctx,
		readMemoriesQuery(s.tableName, limit > 0),
		append(
			[]any{userKey.AppName, userKey.UserID},
			queryLimit(limit)...,
		)...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanMemoryRows(rows)
}

func (s *sqliteMemoryService) SearchMemories(
	ctx context.Context,
	userKey memory.UserKey,
	query string,
	opts ...memory.SearchOption,
) ([]*memory.Entry, error) {
	if err := userKey.CheckUserKey(); err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(
		ctx,
		readMemoriesQuery(s.tableName, false),
		userKey.AppName,
		userKey.UserID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	memories, err := scanMemoryRows(rows)
	if err != nil {
		return nil, err
	}

	return searchMemoryEntries(
		memories,
		memory.ResolveSearchOptions(query, opts),
	), nil
}

func (s *sqliteMemoryService) Tools() []agenttool.Tool {
	return slices.Clone(s.precomputedTools)
}

func (s *sqliteMemoryService) EnqueueAutoMemoryJob(
	ctx context.Context,
	sess *session.Session,
) error {
	if s.autoMemoryWorker == nil {
		return nil
	}
	return s.autoMemoryWorker.EnqueueJob(ctx, sess)
}

func (s *sqliteMemoryService) Close() error {
	if s.autoMemoryWorker != nil {
		s.autoMemoryWorker.Stop()
	}
	if s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *sqliteMemoryService) memoryExists(
	ctx context.Context,
	userKey memory.UserKey,
	memoryID string,
) (bool, error) {
	stmt := fmt.Sprintf(
		`SELECT 1 FROM %s
WHERE app_name = ? AND user_id = ? AND memory_id = ?
AND deleted_at IS NULL LIMIT 1`,
		s.tableName,
	)
	row := s.db.QueryRowContext(
		ctx,
		stmt,
		userKey.AppName,
		userKey.UserID,
		memoryID,
	)

	var exists int
	err := row.Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (s *sqliteMemoryService) countActiveMemories(
	ctx context.Context,
	userKey memory.UserKey,
) (int, error) {
	stmt := fmt.Sprintf(
		`SELECT COUNT(1) FROM %s
WHERE app_name = ? AND user_id = ? AND deleted_at IS NULL`,
		s.tableName,
	)
	row := s.db.QueryRowContext(
		ctx,
		stmt,
		userKey.AppName,
		userKey.UserID,
	)

	var count int
	if err := row.Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func readMemoriesQuery(tableName string, withLimit bool) string {
	stmt := fmt.Sprintf(
		`SELECT app_name, user_id, memory_id, memory_text, topics_json,
%s, created_at, updated_at
FROM %s
WHERE app_name = ? AND user_id = ? AND deleted_at IS NULL
ORDER BY updated_at DESC, created_at DESC`,
		memoryJSONColumnName,
		tableName,
	)
	if withLimit {
		return stmt + "\nLIMIT ?"
	}
	return stmt
}

func queryLimit(limit int) []any {
	if limit <= 0 {
		return nil
	}
	return []any{limit}
}

func scanMemoryRows(rows *sql.Rows) ([]*memory.Entry, error) {
	out := make([]*memory.Entry, 0)
	for rows.Next() {
		entry, err := scanMemoryEntry(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func scanMemoryEntry(scanner interface {
	Scan(dest ...any) error
}) (*memory.Entry, error) {
	var (
		appName   string
		userID    string
		memoryID  string
		memoryTxt string
		topicsRaw string
		memoryRaw sql.NullString
		createdAt string
		updatedAt string
	)
	if err := scanner.Scan(
		&appName,
		&userID,
		&memoryID,
		&memoryTxt,
		&topicsRaw,
		&memoryRaw,
		&createdAt,
		&updatedAt,
	); err != nil {
		return nil, err
	}

	created, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return nil, err
	}
	updated, err := time.Parse(time.RFC3339Nano, updatedAt)
	if err != nil {
		return nil, err
	}
	mem, err := unmarshalStoredMemory(memoryTxt, topicsRaw, memoryRaw, updated)
	if err != nil {
		return nil, err
	}

	return &memory.Entry{
		ID:        memoryID,
		AppName:   appName,
		UserID:    userID,
		Memory:    mem,
		CreatedAt: created,
		UpdatedAt: updated,
	}, nil
}

func (s *sqliteMemoryService) readMemoryEntry(
	ctx context.Context,
	memoryKey memory.Key,
) (*memory.Entry, error) {
	stmt := fmt.Sprintf(
		`SELECT app_name, user_id, memory_id, memory_text, topics_json,
%s, created_at, updated_at
FROM %s
WHERE app_name = ? AND user_id = ? AND memory_id = ?
AND deleted_at IS NULL
LIMIT 1`,
		memoryJSONColumnName,
		s.tableName,
	)
	row := s.db.QueryRowContext(
		ctx,
		stmt,
		memoryKey.AppName,
		memoryKey.UserID,
		memoryKey.MemoryID,
	)
	entry, err := scanMemoryEntry(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf(
			"memory with id %s not found",
			memoryKey.MemoryID,
		)
	}
	if err != nil {
		return nil, err
	}
	return entry, nil
}

func marshalTopics(topics []string) (string, error) {
	cleaned := dedupStrings(topics)
	data, err := json.Marshal(cleaned)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func unmarshalTopics(raw string) ([]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return []string{}, nil
	}
	var topics []string
	if err := json.Unmarshal([]byte(raw), &topics); err != nil {
		return nil, err
	}
	return dedupStrings(topics), nil
}

func marshalMemoryJSON(mem *memory.Memory) (string, error) {
	data, err := json.Marshal(mem)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func unmarshalStoredMemory(
	memoryText string,
	topicsRaw string,
	memoryRaw sql.NullString,
	updated time.Time,
) (*memory.Memory, error) {
	if memoryRaw.Valid && strings.TrimSpace(memoryRaw.String) != "" {
		mem := &memory.Memory{}
		if err := json.Unmarshal([]byte(memoryRaw.String), mem); err != nil {
			return nil, err
		}
		if mem.LastUpdated == nil {
			lastUpdated := updated
			mem.LastUpdated = &lastUpdated
		}
		normalizeMemory(mem)
		return mem, nil
	}

	topics, err := unmarshalTopics(topicsRaw)
	if err != nil {
		return nil, err
	}
	lastUpdated := updated
	mem := &memory.Memory{
		Memory:      memoryText,
		Topics:      topics,
		LastUpdated: &lastUpdated,
	}
	normalizeMemory(mem)
	return mem, nil
}

func rowsAffectedOrNotFound(res sql.Result, memoryID string) error {
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return fmt.Errorf("memory with id %s not found", memoryID)
	}
	return nil
}

func buildMemoryTools(
	ext memextractor.MemoryExtractor,
	enabledTools map[string]struct{},
	cached map[string]agenttool.Tool,
) []agenttool.Tool {
	creators := map[string]memory.ToolCreator{
		memory.AddToolName: func() agenttool.Tool {
			return memorytool.NewAddTool()
		},
		memory.UpdateToolName: func() agenttool.Tool {
			return memorytool.NewUpdateTool()
		},
		memory.DeleteToolName: func() agenttool.Tool {
			return memorytool.NewDeleteTool()
		},
		memory.ClearToolName: func() agenttool.Tool {
			return memorytool.NewClearTool()
		},
		memory.SearchToolName: func() agenttool.Tool {
			return memorytool.NewSearchTool()
		},
		memory.LoadToolName: func() agenttool.Tool {
			return memorytool.NewLoadTool()
		},
	}

	names := make([]string, 0, len(creators))
	for name := range creators {
		if !shouldExposeMemoryTool(name, ext, enabledTools) {
			continue
		}
		names = append(names, name)
	}
	slices.Sort(names)

	out := make([]agenttool.Tool, 0, len(names))
	for _, name := range names {
		if _, ok := cached[name]; !ok {
			cached[name] = creators[name]()
		}
		out = append(out, cached[name])
	}
	return out
}

func shouldExposeMemoryTool(
	name string,
	ext memextractor.MemoryExtractor,
	enabledTools map[string]struct{},
) bool {
	if ext == nil {
		_, ok := enabledTools[name]
		return ok
	}
	switch name {
	case memory.SearchToolName, memory.LoadToolName:
		_, ok := enabledTools[name]
		return ok
	default:
		return false
	}
}

func defaultEnabledTools() map[string]struct{} {
	return map[string]struct{}{
		memory.AddToolName:    {},
		memory.UpdateToolName: {},
		memory.SearchToolName: {},
		memory.LoadToolName:   {},
	}
}

func autoModeEnabledTools() map[string]struct{} {
	return map[string]struct{}{
		memory.AddToolName:    {},
		memory.UpdateToolName: {},
		memory.DeleteToolName: {},
		memory.SearchToolName: {},
	}
}

type enabledToolsConfigurer interface {
	SetEnabledTools(enabled map[string]struct{})
}

func configureExtractorEnabledTools(
	ext memextractor.MemoryExtractor,
	enabled map[string]struct{},
) {
	configurer, ok := ext.(enabledToolsConfigurer)
	if !ok {
		return
	}

	cloned := make(map[string]struct{}, len(enabled))
	for name := range enabled {
		cloned[name] = struct{}{}
	}
	configurer.SetEnabledTools(cloned)
}

type scoredEntry struct {
	entry *memory.Entry
	score float64
}

func logAutoMemoryWarn(
	ctx context.Context,
	format string,
	args ...any,
) {
	agentlog.WarnfContext(ctx, format, args...)
}
