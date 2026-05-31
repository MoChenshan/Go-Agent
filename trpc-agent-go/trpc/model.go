package trpc

import (
	"net/http"

	ihttp "git.woa.com/trpc-go/trpc-agent-go/trpc/internal/http"
	imodel "git.woa.com/trpc-go/trpc-agent-go/trpc/internal/model"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

func init() {
	model.DefaultNewHTTPClient = func(
		opts ...model.HTTPClientOption,
	) model.HTTPClient {
		options := &model.HTTPClientOptions{}
		for _, opt := range opts {
			opt(options)
		}
		if options.Transport != nil {
			return &http.Client{Transport: options.Transport}
		}
		return ihttp.NewRequestHandler(options.Name)
	}

	// Register the context window of Taiji and Hunyuan models.
	model.RegisterModelContextWindows(imodel.ModelContextWindows)

	// Register the context window of Venus models.
	model.RegisterModelContextWindows(imodel.VenusModelContextWindows)
}
