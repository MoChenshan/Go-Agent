package embedder

import (
	"context"
	"os"
	"testing"
	"time"

	"git.woa.com/trpc-go/trpc-agent-go/trpc/knowledge/taiji/sdk"
)

// Global environment variables
var (
	taijiURL            = getEnvOrDefault("TAIJI_URL", "http://stream-server-online-openapi.turbotke.production.polaris:1081")
	taijiToken          = getEnvOrDefault("TAIJI_TOKEN", "")
	taijiWSID           = getEnvOrDefault("TAIJI_WSID", "")
	taijiEmbeddingModel = getEnvOrDefault("TAIJI_EMBEDDING_MODEL", "hunyuan-embedding-public")
)

// getEnvOrDefault gets environment variable, returns default value if not exists
func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func TestEmbedder_GetEmbedding(t *testing.T) {
	// Skip test if environment variables are not set
	if taijiToken == "" || taijiWSID == "" || taijiEmbeddingModel == "" || taijiURL == "" {
		t.Skip("Skipping test: TAIJI_TOKEN, TAIJI_WSID, TAIJI_MODEL, or TAIJI_URL environment variables not set")
	}

	// Table-driven test cases
	tests := []struct {
		name     string
		input    string
		wantErr  bool
		wantDims bool // whether to check dimensions
	}{
		{
			name:     "simple english text",
			input:    "What are some food recommendations near Yudai Lake?",
			wantErr:  false,
			wantDims: true,
		},
		{
			name:     "short text",
			input:    "Hello world",
			wantErr:  false,
			wantDims: true,
		},
		{
			name:     "longer descriptive text",
			input:    "The beautiful scenery around the lake includes mountains, forests, and traditional architecture that attracts many tourists throughout the year.",
			wantErr:  false,
			wantDims: true,
		},
		{
			name:     "single character",
			input:    "A",
			wantErr:  false,
			wantDims: true,
		},
		{
			name:     "empty string",
			input:    "",
			wantErr:  false,
			wantDims: false, // empty string may return empty vector
		},
	}

	// Create embedder instance
	embedder, err := New(WithEmbeddingModel(taijiEmbeddingModel), WithTaijiOption(sdk.TaijiOption{
		WSID:  taijiWSID,
		Token: taijiToken,
		URL:   taijiURL,
	}))
	if err != nil {
		t.Fatalf("Failed to create embedder: %v", err)
	}

	// Run test cases
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			embedding, err := embedder.GetEmbedding(ctx, tt.input)

			// Check error
			if (err != nil) != tt.wantErr {
				t.Errorf("GetEmbedding() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			// If no error expected, check return value
			if !tt.wantErr {
				if embedding == nil {
					t.Error("GetEmbedding() returned nil embedding")
					return
				}

				// Check dimensions
				if tt.wantDims && len(embedding) == 0 {
					t.Error("GetEmbedding() returned empty embedding vector")
				}

				// Check if vector values are reasonable (not all zeros)
				if tt.wantDims && len(embedding) > 0 {
					allZero := true
					for _, val := range embedding {
						if val != 0 {
							allZero = false
							break
						}
					}
					if allZero {
						t.Error("GetEmbedding() returned all-zero embedding vector")
					}
				}

				t.Logf("Input: %q, Embedding dimensions: %d", tt.input, len(embedding))
			}
		})
	}
}

func TestEmbedder_GetEmbeddingWithUsage(t *testing.T) {
	// Skip test if environment variables are not set
	if taijiToken == "" || taijiWSID == "" || taijiEmbeddingModel == "" || taijiURL == "" {
		t.Skip("Skipping test: TAIJI_TOKEN, TAIJI_WSID, TAIJI_MODEL, or TAIJI_URL environment variables not set")
	}

	// Create embedder instance
	embedder, err := New(WithEmbeddingModel(taijiEmbeddingModel), WithTaijiOption(sdk.TaijiOption{
		WSID:  taijiWSID,
		Token: taijiToken,
		URL:   taijiURL,
	}))
	if err != nil {
		t.Fatalf("Failed to create embedder: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	embedding, usage, err := embedder.GetEmbeddingWithUsage(ctx, "What are some food recommendations near Yudai Lake?")
	if err != nil {
		t.Fatalf("GetEmbeddingWithUsage() error = %v", err)
	}

	if embedding == nil {
		t.Error("GetEmbeddingWithUsage() returned nil embedding")
	}

	if len(embedding) == 0 {
		t.Error("GetEmbeddingWithUsage() returned empty embedding vector")
	}

	// usage always returns nil in current implementation
	if usage != nil {
		t.Logf("Usage info: %+v", usage)
	}

	t.Logf("Embedding dimensions: %d", len(embedding))
}

func TestEmbedder_GetDimensions(t *testing.T) {
	// Skip test if environment variables are not set
	if taijiToken == "" || taijiWSID == "" || taijiEmbeddingModel == "" || taijiURL == "" {
		t.Skip("Skipping test: TAIJI_TOKEN, TAIJI_WSID, TAIJI_MODEL, or TAIJI_URL environment variables not set")
	}

	// Create embedder instance
	embedder, err := New(WithEmbeddingModel(taijiEmbeddingModel), WithTaijiOption(sdk.TaijiOption{
		WSID:  taijiWSID,
		Token: taijiToken,
		URL:   taijiURL,
	}))
	if err != nil {
		t.Fatalf("Failed to create embedder: %v", err)
	}

	// First call should trigger actual API request to get dimensions
	dims := embedder.GetDimensions()
	if dims <= 0 {
		t.Errorf("GetDimensions() returned invalid dimensions: %d", dims)
	}

	// Second call should use cached dimensions
	dims2 := embedder.GetDimensions()
	if dims != dims2 {
		t.Errorf("GetDimensions() returned inconsistent dimensions: first=%d, second=%d", dims, dims2)
	}

	t.Logf("Embedding dimensions: %d", dims)
}

func TestEmbedder_New_InvalidConfig(t *testing.T) {
	tests := []struct {
		name           string
		option         sdk.TaijiOption
		embeddingModel string
	}{
		{
			name:   "empty config",
			option: sdk.TaijiOption{},
		},
		{
			name: "missing token",
			option: sdk.TaijiOption{
				WSID: "test-wsid",
				URL:  "http://test.com",
			},
			embeddingModel: "test-model",
		},
		{
			name: "missing wsid",
			option: sdk.TaijiOption{
				Token: "test-token",
				URL:   "http://test.com",
			},
			embeddingModel: "test-model",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			embedder, err := New(WithTaijiOption(tt.option), WithEmbeddingModel(tt.embeddingModel))
			if err != nil {
				t.Errorf("New() error = %v, expected no error during creation", err)
				return
			}

			// Attempting to get embedding should fail
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			_, err = embedder.GetEmbedding(ctx, "test")
			if err == nil {
				t.Error("GetEmbedding() expected error with invalid config, got nil")
			}
		})
	}
}
