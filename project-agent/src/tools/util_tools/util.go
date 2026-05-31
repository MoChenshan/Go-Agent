// Package util 提供 GameOps Agent 使用的通用工具函数。
//
// 本文件提供时间戳与 Base64 两个最常用的工具函数，
// 复用自 oncall_agent/utils/{time.go,base64.go} 的思路，做精简化改造。
package util

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"time"
)

// ParseTimestampMilli 将任意形式的时间戳输入（string / int64 / float64）
// 转为毫秒时间戳。
// 支持：
//   - 毫秒时间戳（13 位数字）
//   - 秒级时间戳（10 位数字，自动 x1000）
//   - RFC3339 字符串："2026-01-02T15:04:05+08:00"
//   - 常见格式字符串："2026-01-02 15:04:05"
func ParseTimestampMilli(v any) (int64, error) {
	switch x := v.(type) {
	case int64:
		return normalizeMilli(x), nil
	case int:
		return normalizeMilli(int64(x)), nil
	case float64:
		return normalizeMilli(int64(x)), nil
	case string:
		// 先尝试数字
		if n, err := strconv.ParseInt(x, 10, 64); err == nil {
			return normalizeMilli(n), nil
		}
		// RFC3339
		if t, err := time.Parse(time.RFC3339, x); err == nil {
			return t.UnixMilli(), nil
		}
		// 常见中文时间
		if t, err := time.ParseInLocation("2006-01-02 15:04:05", x, time.Local); err == nil {
			return t.UnixMilli(), nil
		}
		return 0, fmt.Errorf("unrecognized timestamp format: %q", x)
	default:
		return 0, fmt.Errorf("unsupported timestamp type: %T", v)
	}
}

// normalizeMilli 把秒级时间戳（10 位）归一化到毫秒级（13 位）。
func normalizeMilli(n int64) int64 {
	// 1e11 ≈ 2286 年，10 位时间戳不超过它
	if n < 1e11 {
		return n * 1000
	}
	return n
}

// EncodeBase64 把 bytes 编码为标准 Base64 字符串。
func EncodeBase64(b []byte) string {
	return base64.StdEncoding.EncodeToString(b)
}

// DecodeBase64 解码标准 Base64 字符串为 bytes。
func DecodeBase64(s string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(s)
}
