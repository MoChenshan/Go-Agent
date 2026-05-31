package trpc_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	trpc "git.code.oa.com/trpc-go/trpc-go"
	_ "git.woa.com/trpc-go/trpc-agent-go/trpc"
	"trpc.group/trpc-go/trpc-agent-go/log"
)

func Test_log_int(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "log.log")
	config := `plugins:
  log:
    default:
      - writer: console
        level: error
      - writer: file  # 本地文件日志
        write_mode: 1
        level: warn  # 本地文件滚动日志的级别
        writer_config:
          log_path: ` + logFile

	configFile := filepath.Join(tmpDir, "trpc_go.yaml")
	err := os.WriteFile(configFile, []byte(config), 0644)
	if err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}
	serverConfigPath := trpc.ServerConfigPath
	trpc.ServerConfigPath = configFile
	t.Cleanup(func() {
		trpc.ServerConfigPath = serverConfigPath
	})
	// load the config file
	_ = trpc.NewServer()
	log.Info("test log info")
	log.Warn("test log warn")
	require.Eventually(t, func() bool {
		content, err := os.ReadFile(logFile)
		if err != nil {
			return false
		}
		return len(content) > 0
	}, 500*time.Millisecond, 50*time.Millisecond)

	content, err := os.ReadFile(logFile)
	require.NoError(t, err)
	require.NotContains(t, string(content), "test log info")
	require.Contains(t, string(content), "test log warn")
}
