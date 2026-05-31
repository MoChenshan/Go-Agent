package text2image

import (
	"context"
	"os"

	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewToolSet(t *testing.T) {
	setEnv()
	ctx := context.Background()

	// Create a toolset with specific functions
	toolset, err := NewToolSet(
		ctx,
		WithAPIKey("test-api-key"),
		WithName("image generation"),
	)
	require.NoError(t, err)
	defer toolset.Close()

	tools := toolset.Tools(ctx)
	require.Len(t, tools, 1)
	require.Equal(t, "text2image", tools[0].Declaration().Name)

	t.Logf("tools[0].Declaration().InputSchema(): %+v", tools[0].Declaration().InputSchema)
}

func setEnv() {
	os.Setenv("OPENAI_API_KEY", "test-api-key")
}
