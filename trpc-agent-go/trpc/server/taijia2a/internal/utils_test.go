package internal

import (
	"testing"
)

// TestRepairBasePath 测试 repairBasePath 函数
func TestRepairBasePath(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "空字符串",
			input:    "",
			expected: "",
		},
		{
			name:     "只有斜杠",
			input:    "/",
			expected: "",
		},
		{
			name:     "开头有斜杠",
			input:    "/api",
			expected: "api/",
		},
		{
			name:     "结尾有斜杠",
			input:    "api/",
			expected: "api/",
		},
		{
			name:     "开头结尾都有斜杠",
			input:    "/api/",
			expected: "api/",
		},
		{
			name:     "正常路径无斜杠",
			input:    "api",
			expected: "api/",
		},
		{
			name:     "多级路径开头有斜杠",
			input:    "/api/v1",
			expected: "api/v1/",
		},
		{
			name:     "多级路径结尾有斜杠",
			input:    "api/v1/",
			expected: "api/v1/",
		},
		{
			name:     "多级路径开头结尾都有斜杠",
			input:    "/api/v1/",
			expected: "api/v1/",
		},
		{
			name:     "连续斜杠开头",
			input:    "//api",
			expected: "/api/",
		},
		{
			name:     "连续斜杠结尾",
			input:    "api//",
			expected: "api//",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := RepairBasePath(tt.input)
			if result != tt.expected {
				t.Errorf("repairBasePath(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}
