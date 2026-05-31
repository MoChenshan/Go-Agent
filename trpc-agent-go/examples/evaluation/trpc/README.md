# Online Evaluation tRPC Server Example

This example exposes the existing evaluation workflow through a tRPC `http_no_protocol` service so web pages or other systems can trigger evaluation runs remotely instead of executing them only from the CLI.

## Environment Variables

| Variable | Description | Default Value |
|----------|-------------|---------------|
| `OPENAI_API_KEY` | API key for the model service (required) | `` |
| `OPENAI_BASE_URL` | Base URL for the model API endpoint | `https://api.openai.com/v1` |

## Configuration Flags

| Flag | Description | Default |
|------|-------------|---------|
| `-base-path` | Base path exposed by the evaluation server | `/evaluation` |
| `-model` | Model identifier used by the calculator agent | `deepseek-v4-flash` |
| `-streaming` | Enable streaming responses from the agent | `false` |
| `-data-dir` | Directory containing `.evalset.json` and `.metrics.json` files | `../server/data` |
| `-output-dir` | Directory where evaluation results are written | `./output` |

The listen address is configured by `trpc_go.yaml`. The default service listens on `127.0.0.1:8080` with service name `trpc.test.evaluation.trpc`.

## Run

```bash
cd trpc-agent-go/examples/evaluation/trpc
go run . \
  -base-path "/evaluation" \
  -model "deepseek-v4-flash" \
  -data-dir "../server/data" \
  -output-dir "./output"
```

The server exposes the following endpoints:

- `GET /evaluation/sets`
- `GET /evaluation/sets/{setId}`
- `POST /evaluation/runs`
- `GET /evaluation/results`
- `GET /evaluation/results/{resultId}`

## Example Requests

List available evaluation sets:

```bash
curl "http://127.0.0.1:8080/evaluation/sets"
```

Run an evaluation set online:

```bash
curl -X POST "http://127.0.0.1:8080/evaluation/runs" \
  -H "Content-Type: application/json" \
  -d '{"setId":"math-basic","numRuns":1}'
```

The response body contains the `evaluationResult` returned by the agent evaluator.
