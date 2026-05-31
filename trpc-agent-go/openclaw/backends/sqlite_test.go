package backends

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/memory"
	memextractor "trpc.group/trpc-go/trpc-agent-go/memory/extractor"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/openclaw/registry"
	"trpc.group/trpc-go/trpc-agent-go/session"
)

func TestSQLiteMemoryBackendRegistered(t *testing.T) {
	_, ok := registry.LookupMemoryBackend(memoryBackendSQLite)
	require.True(t, ok)
}

func TestSQLiteMemoryServiceCRUD(t *testing.T) {
	svc := newTestSQLiteService(t, nil)
	ctx := context.Background()
	userKey := memory.UserKey{
		AppName: "app",
		UserID:  "user",
	}

	err := svc.AddMemory(ctx, userKey, "User likes coffee", []string{"drink"})
	require.NoError(t, err)

	entries, err := svc.ReadMemories(ctx, userKey, 10)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, "User likes coffee", entries[0].Memory.Memory)
	require.Equal(t, []string{"drink"}, entries[0].Memory.Topics)

	key := memory.Key{
		AppName:  userKey.AppName,
		UserID:   userKey.UserID,
		MemoryID: entries[0].ID,
	}
	updateResult := &memory.UpdateResult{}
	err = svc.UpdateMemory(
		ctx,
		key,
		"User likes tea",
		[]string{"drink", "preference"},
		memory.WithUpdateResult(updateResult),
	)
	require.NoError(t, err)
	key.MemoryID = updateResult.MemoryID

	entries, err = svc.ReadMemories(ctx, userKey, 10)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, "User likes tea", entries[0].Memory.Memory)
	require.Equal(
		t,
		[]string{"drink", "preference"},
		entries[0].Memory.Topics,
	)

	err = svc.DeleteMemory(ctx, key)
	require.NoError(t, err)

	entries, err = svc.ReadMemories(ctx, userKey, 10)
	require.NoError(t, err)
	require.Empty(t, entries)
}

func TestSQLiteMemoryServiceSearchAndLimit(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), testDBFileName)
	svc := newTestSQLiteService(t, &registry.MemoryBackendSpec{
		Type:  memoryBackendSQLite,
		Limit: 1,
		Config: yamlNode(t, `
path: "`+dbPath+`"
`),
	})
	ctx := context.Background()
	userKey := memory.UserKey{
		AppName: "app",
		UserID:  "user",
	}

	err := svc.AddMemory(
		ctx,
		userKey,
		"User likes coffee and cakes",
		[]string{"food"},
	)
	require.NoError(t, err)

	err = svc.AddMemory(
		ctx,
		userKey,
		"User works as a developer",
		[]string{"work"},
	)
	require.Error(t, err)

	results, err := svc.SearchMemories(ctx, userKey, "coffee")
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, "User likes coffee and cakes", results[0].Memory.Memory)
}

func TestSQLiteMemoryServiceSearchChinese(t *testing.T) {
	svc := newTestSQLiteService(t, nil)
	ctx := context.Background()
	userKey := memory.UserKey{
		AppName: "app",
		UserID:  "user",
	}

	err := svc.AddMemory(
		ctx,
		userKey,
		"用户喜欢企业微信流式回复体验",
		[]string{"wecom"},
	)
	require.NoError(t, err)

	results, err := svc.SearchMemories(ctx, userKey, "企业微信流式")
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(
		t,
		"用户喜欢企业微信流式回复体验",
		results[0].Memory.Memory,
	)
}

func TestSQLiteMemoryBackendDefaultConfigWorks(t *testing.T) {
	tempDir := t.TempDir()
	oldWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tempDir))
	t.Cleanup(func() {
		require.NoError(t, os.Chdir(oldWD))
	})

	svc, err := newSQLiteMemoryBackend(
		registry.MemoryDeps{},
		registry.MemoryBackendSpec{Type: memoryBackendSQLite},
	)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, svc.Close())
	})

	sqliteSvc := svc.(*sqliteMemoryService)
	userKey := memory.UserKey{
		AppName: "app",
		UserID:  "user",
	}
	err = sqliteSvc.AddMemory(
		context.Background(),
		userKey,
		"Default config memory",
		nil,
	)
	require.NoError(t, err)

	_, err = os.Stat(filepath.Join(tempDir, defaultSQLiteMemoryDBFile))
	require.NoError(t, err)
}

func TestSQLiteMemoryAutoMode(t *testing.T) {
	ext := &staticExtractor{
		ops: []*memextractor.Operation{{
			Type:   memextractor.OperationAdd,
			Memory: "User likes green tea",
			Topics: []string{"drink"},
		}},
	}
	svc := newTestSQLiteService(t, &registry.MemoryBackendSpec{
		Type: memoryBackendSQLite,
		Config: yamlNode(t, `
path: ":memory:"
`),
	}, ext)
	ctx := context.Background()
	sess := session.NewSession("app", "user", "sess")
	sess.Events = append(sess.Events, event.Event{
		Timestamp: time.Now(),
		Response: &model.Response{
			Choices: []model.Choice{{
				Message: model.Message{
					Role:    model.RoleUser,
					Content: "Remember that I like green tea",
				},
			}},
		},
	})

	err := svc.EnqueueAutoMemoryJob(ctx, sess)
	require.NoError(t, err)

	userKey := memory.UserKey{
		AppName: "app",
		UserID:  "user",
	}
	require.Eventually(t, func() bool {
		entries, readErr := svc.ReadMemories(ctx, userKey, 10)
		if readErr != nil || len(entries) != 1 {
			return false
		}
		return entries[0].Memory.Memory == "User likes green tea"
	}, time.Second, 20*time.Millisecond)

	require.Len(t, svc.Tools(), 1)
}

func TestSQLiteMemoryServiceMetadataRoundTrip(t *testing.T) {
	svc := newTestSQLiteService(t, nil)
	ctx := context.Background()
	userKey := memory.UserKey{
		AppName: "app",
		UserID:  "user",
	}
	eventTime := time.Date(
		2026,
		time.March,
		17,
		9,
		30,
		0,
		0,
		time.UTC,
	)

	err := svc.AddMemory(
		ctx,
		userKey,
		"Met Alice in Shanghai",
		[]string{"travel"},
		memory.WithMetadata(&memory.Metadata{
			Kind:         memory.KindEpisode,
			EventTime:    &eventTime,
			Participants: []string{"Alice", " alice ", "Bob"},
			Location:     " Shanghai ",
		}),
	)
	require.NoError(t, err)

	entries, err := svc.ReadMemories(ctx, userKey, 10)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, memory.KindEpisode, entries[0].Memory.Kind)
	require.Equal(
		t,
		[]string{"Alice", "Bob"},
		entries[0].Memory.Participants,
	)
	require.Equal(t, "Shanghai", entries[0].Memory.Location)
	require.NotNil(t, entries[0].Memory.EventTime)
	require.True(t, entries[0].Memory.EventTime.Equal(eventTime))

	key := memory.Key{
		AppName:  userKey.AppName,
		UserID:   userKey.UserID,
		MemoryID: entries[0].ID,
	}
	nextEventTime := eventTime.Add(2 * time.Hour)
	updateResult := &memory.UpdateResult{}
	err = svc.UpdateMemory(
		ctx,
		key,
		"Met Alice and Bob in Shanghai",
		[]string{"travel", "friend"},
		memory.WithUpdateMetadata(&memory.Metadata{
			EventTime: &nextEventTime,
		}),
		memory.WithUpdateResult(updateResult),
	)
	require.NoError(t, err)
	require.NotEmpty(t, updateResult.MemoryID)
	require.NotEqual(t, key.MemoryID, updateResult.MemoryID)

	entries, err = svc.ReadMemories(ctx, userKey, 10)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, updateResult.MemoryID, entries[0].ID)
	require.Equal(
		t,
		"Met Alice and Bob in Shanghai",
		entries[0].Memory.Memory,
	)
	require.Equal(
		t,
		[]string{"travel", "friend"},
		entries[0].Memory.Topics,
	)
	require.Equal(
		t,
		[]string{"Alice", "Bob"},
		entries[0].Memory.Participants,
	)
	require.NotNil(t, entries[0].Memory.EventTime)
	require.True(t, entries[0].Memory.EventTime.Equal(nextEventTime))
}

func TestSQLiteMemoryServiceSearchOptions(t *testing.T) {
	svc := newTestSQLiteService(t, nil)
	ctx := context.Background()
	userKey := memory.UserKey{
		AppName: "app",
		UserID:  "user",
	}
	earlier := time.Date(
		2026,
		time.March,
		1,
		10,
		0,
		0,
		0,
		time.UTC,
	)
	later := earlier.Add(48 * time.Hour)

	err := svc.AddMemory(
		ctx,
		userKey,
		"User planned a Kyoto trip",
		[]string{"travel"},
		memory.WithMetadata(&memory.Metadata{
			Kind:      memory.KindEpisode,
			EventTime: &later,
		}),
	)
	require.NoError(t, err)
	err = svc.AddMemory(
		ctx,
		userKey,
		"User booked a Kyoto hotel",
		[]string{"travel"},
		memory.WithMetadata(&memory.Metadata{
			Kind:      memory.KindEpisode,
			EventTime: &earlier,
		}),
	)
	require.NoError(t, err)
	err = svc.AddMemory(
		ctx,
		userKey,
		"User likes Kyoto tea",
		[]string{"food"},
	)
	require.NoError(t, err)

	results, err := svc.SearchMemories(
		ctx,
		userKey,
		"Kyoto",
		memory.WithSearchOptions(memory.SearchOptions{
			Query:            "Kyoto",
			Kind:             memory.KindEpisode,
			OrderByEventTime: true,
		}),
	)
	require.NoError(t, err)
	require.Len(t, results, 2)
	require.Equal(t, "User booked a Kyoto hotel", results[0].Memory.Memory)
	require.Equal(t, "User planned a Kyoto trip", results[1].Memory.Memory)

	results, err = svc.SearchMemories(
		ctx,
		userKey,
		"Kyoto",
		memory.WithSearchOptions(memory.SearchOptions{
			Query:            "Kyoto",
			Kind:             memory.KindEpisode,
			TimeAfter:        &later,
			OrderByEventTime: true,
		}),
	)
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, "User planned a Kyoto trip", results[0].Memory.Memory)

	results, err = svc.SearchMemories(
		ctx,
		userKey,
		"Kyoto",
		memory.WithSearchOptions(memory.SearchOptions{
			Query:        "Kyoto",
			Kind:         memory.KindFact,
			KindFallback: true,
		}),
	)
	require.NoError(t, err)
	require.Len(t, results, 3)
	require.Equal(t, "User likes Kyoto tea", results[0].Memory.Memory)
}

func TestSQLiteMemoryServiceLegacyRowsRemainReadable(t *testing.T) {
	svc := newTestSQLiteService(t, nil)
	ctx := context.Background()
	createdAt := time.Date(
		2026,
		time.March,
		17,
		8,
		0,
		0,
		0,
		time.UTC,
	)
	updatedAt := createdAt.Add(30 * time.Minute)

	_, err := svc.db.ExecContext(
		ctx,
		`INSERT INTO openclaw_memories (
app_name, user_id, memory_id, memory_text, topics_json,
created_at, updated_at, deleted_at
) VALUES (?, ?, ?, ?, ?, ?, ?, NULL)`,
		"app",
		"user",
		"legacy-id",
		"Legacy memory row",
		`["legacy"]`,
		createdAt.Format(time.RFC3339Nano),
		updatedAt.Format(time.RFC3339Nano),
	)
	require.NoError(t, err)

	entries, err := svc.ReadMemories(
		ctx,
		memory.UserKey{
			AppName: "app",
			UserID:  "user",
		},
		10,
	)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, "legacy-id", entries[0].ID)
	require.Equal(t, "Legacy memory row", entries[0].Memory.Memory)
	require.Equal(t, []string{"legacy"}, entries[0].Memory.Topics)
	require.Equal(t, memory.KindFact, entries[0].Memory.Kind)
	require.True(t, entries[0].UpdatedAt.Equal(updatedAt))
}

const (
	testDBFileName            = "test-memories.db"
	defaultSQLiteMemoryDBFile = "memories.db"
)

func newTestSQLiteService(
	t *testing.T,
	spec *registry.MemoryBackendSpec,
	extractors ...memextractor.MemoryExtractor,
) *sqliteMemoryService {
	t.Helper()

	if spec == nil {
		spec = &registry.MemoryBackendSpec{
			Type: memoryBackendSQLite,
			Config: yamlNode(t, `
path: ":memory:"
`),
		}
	}

	var ext memextractor.MemoryExtractor
	if len(extractors) > 0 {
		ext = extractors[0]
	}

	svc, err := newSQLiteMemoryBackend(
		registry.MemoryDeps{
			AppName:   "app",
			Extractor: ext,
		},
		*spec,
	)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, svc.Close())
	})

	return svc.(*sqliteMemoryService)
}

func yamlNode(t *testing.T, content string) *yaml.Node {
	t.Helper()

	var node yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(content), &node))
	if len(node.Content) == 0 {
		return nil
	}
	return node.Content[0]
}

type staticExtractor struct {
	ops []*memextractor.Operation
}

func (s *staticExtractor) Extract(
	_ context.Context,
	_ []model.Message,
	_ []*memory.Entry,
) ([]*memextractor.Operation, error) {
	return s.ops, nil
}

func (s *staticExtractor) ShouldExtract(
	_ *memextractor.ExtractionContext,
) bool {
	return true
}

func (s *staticExtractor) SetPrompt(_ string) {}

func (s *staticExtractor) SetModel(_ model.Model) {}

func (s *staticExtractor) Metadata() map[string]any {
	return map[string]any{}
}
