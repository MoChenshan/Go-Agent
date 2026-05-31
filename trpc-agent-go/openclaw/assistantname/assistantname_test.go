package assistantname

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalize(t *testing.T) {
	t.Parallel()

	require.Equal(t, "", Normalize(""))
	require.Equal(t, "Claw", Normalize("  \"Claw\"  "))
	require.Equal(t, "My Claw", Normalize(" My   Claw "))

	raw := "0123456789012345678901234567890123456789"
	require.Len(t, []rune(Normalize(raw)), MaxRunes)
}

func TestIsResetToken(t *testing.T) {
	t.Parallel()

	require.True(t, IsResetToken(""))
	require.True(t, IsResetToken("off"))
	require.True(t, IsResetToken("clear"))
	require.True(t, IsResetToken("default"))
	require.True(t, IsResetToken("reset"))
	require.False(t, IsResetToken("claw"))
}

func TestReadWriteFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), FileName)

	name, err := ReadFile(path)
	require.NoError(t, err)
	require.Empty(t, name)

	require.NoError(t, WriteFile(path, "  Claw  "))
	name, err = ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "Claw", name)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "Claw\n", string(data))

	require.NoError(t, WriteFile(path, ""))
	name, err = ReadFile(path)
	require.NoError(t, err)
	require.Empty(t, name)
}
