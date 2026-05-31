package e2e

import (
	"context"
	"errors"

	"trpc.group/trpc-go/trpc-agent-go/model"
)

type Call struct {
	Responses []*model.Response
}

type QueueModel struct {
	Calls []Call
}

func (m *QueueModel) Push(call Call) {
	m.Calls = append(m.Calls, call)
}

func (m *QueueModel) GenerateContent(ctx context.Context, request *model.Request) (<-chan *model.Response, error) {
	if request == nil {
		return nil, errors.New("mock model: request is nil")
	}
	if len(m.Calls) == 0 {
		return nil, errors.New("mock model: no queued calls")
	}

	ch := make(chan *model.Response)
	go func() {
		defer close(ch)
		for _, call := range m.Calls {
			for _, resp := range call.Responses {
				select {
				case <-ctx.Done():
					return
				case ch <- resp:
				}
			}
		}
	}()
	return ch, nil
}

func (m *QueueModel) Info() model.Info {
	return model.Info{Name: "mock-model"}
}
