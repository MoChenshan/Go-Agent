// Package model 包含跨领域共享的领域对象和原语
package model

import agentmodel "trpc.group/trpc-go/trpc-agent-go/model"

// GenConfig 大模型生成配置参数（从七彩石提取，由 main.go 注入）
type GenConfig struct {
	// Temperature 控制生成随机性（0.0 到 2.0）
	Temperature float64
	// TopP 核采样参数（0.0 到 1.0）
	TopP float64
}

// BuildGenConfig 构建大模型生成配置
func BuildGenConfig(cfg GenConfig) agentmodel.GenerationConfig {
	genConfig := agentmodel.GenerationConfig{
		Stream: true, // 启用流式输出
	}
	if cfg.Temperature != 0 {
		temp := cfg.Temperature
		genConfig.Temperature = &temp
	}
	if cfg.TopP != 0 {
		topP := cfg.TopP
		genConfig.TopP = &topP
	}
	return genConfig
}
