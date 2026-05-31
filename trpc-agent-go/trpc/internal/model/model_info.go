// Package model defines the context window of internal models.
package model

// ModelContextWindows contains the context window sizes for Taiji and Hunyuan models.
var ModelContextWindows = map[string]int{
	// Hunyuan
	// https://iwiki.woa.com/p/4010715517
	"hy3-preview":                             256000,
	"hunyuan-2.0-thinking-20251109":           192000,
	"hunyuan-2.0-instruct-20251111":           144000,
	"hunyuan-t1-latest":                       96000,
	"hunyuan-t1-20250822":                     96000,
	"hunyuan-t1-20250711":                     92000,
	"hunyuan-t1-20250715":                     92000,
	"hunyuan-t1-20250529":                     92000,
	"hunyuan-turbos-latest":                   48000,
	"hunyuan-turbos-20250926":                 48000,
	"hunyuan-turbos-20250716":                 48000,
	"hunyuan-turbos-20250604":                 48000,
	"hunyuan-turbos-longtext-128k-20250325":   128000,
	"hunyuan-large":                           32000,
	"hunyuan-standard":                        32000,
	"hunyuan-standard-256K":                   256000,
	"hunyuan-0.5b":                            256000,
	"hunyuan-1.8b":                            256000,
	"hunyuan-4b":                              256000,
	"hunyuan-7b":                              256000,
	"hunyuan-13b-20250613":                    256000,
	"hunyuan-lite-13b":                        32000,
	"hunyuan-7b-20250613":                     256000,
	"hunyuan-a13b":                            256000,
	"hunyuan-mt-7b":                           8000,
	"hunyuan-mt-chimera-7b":                   28000,
	"hunyuan-mt-1.8b":                         8000,
	"hunyuan-role-latest":                     32600,
	"hunyuan-embedding-20250716":              1000,
	"hunyuan-2.0-thinking-codeagent-20251104": 192000,
	"hunyuan-codecmp":                         16384,
	"hunyuan-t1-vision-20250916":              40000,
	"hunyuan-t1-vision-latest":                40000,
	"hunyuan-t1-vision-20250619":              40000,
	"HY-Vision-2.0-instruct":                  44000,
	"HY-vision-1.5-instruct":                  32000,
	"hunyuan-turbos-vision-latest":            32000,
	"hunyuan-turbos-vision-20250619":          32000,
	"hunyuan-vision-7b-20250720":              40000,
	"HY-OCR-1.0":                              8000,
	"hunyuan-ocr":                             8000,
	"hunyuan-ocr-1b-edu-20251125":             8000,
	"hunyuan-turbos-vision-video-latest":      32768,
	"hunyuan-translation":                     8000,
	"hunyuan-translation-lite":                8000,
	"hunyuan-funcall":                         32000,

	// Taiji
	"DeepSeek-V3-Online-128K":       128000,
	"DeepSeek-V3-Online-64K":        64000,
	"DeepSeek-V3-Online":            32000,
	"DeepSeek-V3-Online-16K":        16000,
	"DeepSeek-R1-Online-128K":       128000,
	"DeepSeek-R1-Online-64K":        64000,
	"DeepSeek-R1-Online":            32000,
	"DeepSeek-R1-Online-16K":        16000,
	"DeepSeek-V3_1-Online-128k":     128000,
	"DeepSeek-V3_1-Online-64k":      64000,
	"DeepSeek-V3_1-Online-32k":      32000,
	"DeepSeek-V3_1-Online-16k":      16000,
	"DeepSeek-V3_2-Online-128k":     128000,
	"DeepSeek-V3_2-Online-64k":      64000,
	"DeepSeek-V3_2-Online-32k":      32000,
	"DeepSeek-V3_2-Online-16k":      16000,
	"DeepSeek-V4-Pro-Online-32k":    32000,
	"DeepSeek-V4-Flash-Online-128k": 128000,
	"DeepSeek-V4-Flash-Online-32k":  32000,
	"DeepSeek-V4-Flash-Online-16k":  16000,
}

// VenusModelContextWindows contains the context window sizes for Venus models.
// Reference: https://iwiki.woa.com/p/4007939522
// Visit it in browser: http://venus.woa.com/venusapi/chat/model/list?appGroupId=1
var VenusModelContextWindows = map[string]int{
	// DeepSeek
	"deepseek-v3.1-terminus":       65536,
	"deepseek-v3.2":                65536,
	"deepseek-v4-flash":            131072,
	"deepseek-v4-pro":              131072,
	"deepseek-r1-distill-qwen-32b": 16384,
	"deepseek-r1-local-II":         65536,
	"deepseek-r1-local-III":        65536,
	"deepseek-v3-local-II":         65536,
	"deepseek-ocr":                 8192,
	"deepseek-v4-flash-external":   1048576,
	"deepseek-v4-pro-external":     1048576,
	"deepseek-v3-0324-local":       65536,
	"deepseek-r1-local-0528":       65536,

	// Hunyuan
	"hy3-preview":                  131072,
	"hunyuan-turbo":                28672,
	"hunyuan-standard":             30720,
	"hunyuan-standard-70b":         28672,
	"hunyuan-codecmp":              15872,
	"hunyuan-standard-256K":        262144,
	"hunyuan-t1-32k":               28672,
	"hunyuan-turbos-vision-latest": 16384,
	"hunyuan-turbos-latest":        24576,

	// GLM
	"glm-5":                131072,
	"glm-5.1":              131072,
	"GLM-4.1V-9B-Thinking": 32768,
	"glm-4.7":              131072,

	// Qwen
	"qwen3-32b-fp8":               40960,
	"qwen3-30b-a3b-instruct-2507": 65536,
	"qwen3-30b-a3b-thinking-2507": 65536,
	"qwen3-omni-30b-a3b-thinking": 65536,
	"qwen3-omni-30b-a3b-instruct": 65536,
	"qwen2.5-vl-32b-instruct":     10240,
	"qwen3-235b-a22b-2507-fp8":    131072,
	"qwen3-vl-235b-a22b-thinking": 131072,
	"qwen3-vl-235b-a22b-instruct": 131072,
	"qwen3.5-35b-a3b":             65536,
	"qwen3.5-397b-a17b":           65536,
	"qwen3.6-35b-a3b":             65536,

	// Kimi
	"kimi-k2-light": 65536,
	"kimi-k2.5":     131072,
	"kimi-k2.6":     131072,

	// Venus
	"venus-qa-13b":     4096,
	"venus-qa-14b":     4096,
	"venus-qa-2.5-14b": 4096,

	// MiniMax
	"minimax-m2.5": 131072,
	"minimax-m2.7": 131072,

	// OpenAI
	"gpt-5":             272000,
	"gpt-5.1":           400000,
	"gpt-5.1-chat":      400000,
	"gpt-5.2":           400000,
	"gpt-5.2-chat":      400000,
	"gpt-5.4":           1050000,
	"gpt-5.4-mini":      400000,
	"gpt-5.4-nano":      400000,
	"gpt-5.5":           1050000,
	"gpt-4o":            128000,
	"gpt-4o-mini":       131072,
	"gpt-4o-2024-08-06": 128000,
	"gpt-4o-2024-11-20": 128000,
	"gpt-4.1":           1047576,
	"gpt-4.1-mini":      1047576,
	"gpt-5-mini":        272000,
	"gpt-5-nano":        272000,
	"gpt-5-chat":        128000,

	// OpenAI O-Series
	"o3-mini": 128000,
	"o3":      200000,
	"o4-mini": 200000,

	// ARC
	"arc-video-7b-v1.1": 2048,
	"arc-video-7b-v2":   2048,

	// Claude
	"claude-opus-4-6":            1024000,
	"claude-sonnet-4-6":          131072,
	"claude-opus-4-7":            1024000,
	"claude-4-sonnet-20250514":   200000,
	"claude-4-5-sonnet-20250929": 131072,
	"claude-4-5-haiku-20251001":  200000,
	"claude-opus-4-5-20251101":   131072,

	// Gemini
	"gemini-2.5-pro":         1048576,
	"gemini-2.5-flash":       1048576,
	"gemini-3-pro-image":     1048576,
	"gemini-3-flash":         1048576,
	"gemini-3.1-pro":         1048576,
	"gemini-3.1-flash-lite":  1048576,
	"gemini-3.1-flash-image": 1048576,
	"gemini-2.0-flash":       102400,
	"gemini-2.5-flash-image": 1048576,
	"gemma-4-31b-it":         65536,
	"gemma-4-26b-a4b-it":     65536,

	// Doubao
	"doubao-1-5-thinking-vision-pro-250428": 12288,
	"doubao-1.5-pro-32k-250115":             12288,

	// Grok
	"grok-3": 131072,
}
