package evaluation

import (
	"git.woa.com/galileo/eco/go/sdk/base/model"
	"git.woa.com/galileo/trpc-agent-go-galileo/evaluation"
)

// Setup Set evaluation parameters
func Setup(res model.Resource, address string) error {
	return evaluation.Setup(&res, address)
}
