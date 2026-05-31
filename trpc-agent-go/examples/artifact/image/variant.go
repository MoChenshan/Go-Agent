package main

import (
	"context"
	"fmt"
	"os"

	"git.woa.com/trpc-go/trpc-agent-go/trpc/tool/hunyuan/text2image"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

type variant string

const (
	// VariantDefaultMock is the default mock text2image variant.
	VariantDefaultMock variant = "default_mock"
	// VariantHunyuan is the hunyuan text2image variant.
	VariantHunyuan variant = "hunyuan"
)

func getVariantConfig(ctx context.Context, v variant) (variantConfig, error) {
	switch v {
	case VariantDefaultMock:
		return variantConfig{
			tools: []tool.Tool{
				generateImageTool,
				displayImageTool,
			},
			instruction: `When the user requests an image,
first rewrite and optimize the prompt in English, 
then call text-to-image tool to generate it, 
finally call display-image tool to display it.`,
		}, nil
	case VariantHunyuan:
		setEnv()
		toolset, err := text2image.NewToolSet(ctx)
		if err != nil {
			return variantConfig{}, err
		}
		return variantConfig{
			tools: toolset.Tools(ctx),
			instruction: `When the user requests an image,
first rewrite and optimize the prompt, 
then call text-to-image tool to generate it`,
		}, nil
	default:
		return variantConfig{}, fmt.Errorf("invalid variant: %s", v)
	}
}

type variantConfig struct {
	tools       []tool.Tool
	instruction string
}

func setEnv() {
	os.Setenv("HUNYUAN_IMAGE_API_KEY", "your-image-api-key")  // e.g.  your-image-api-key
	os.Setenv("HUNYUAN_IMAGE_MODEL", "your-base-image-model") // e.g.  hunyuan-image-v3.0-v1.0.4
	os.Setenv("HUNYUAN_IMAGE_API_URL", "your-image-api-url")  // e.g.  http://hunyuanapi.woa.com
	os.Setenv("HUNYUAN_IMAGE_PATH", "your-image-path")        // e.g.  /openapi/v1/images/ar/generations
}
