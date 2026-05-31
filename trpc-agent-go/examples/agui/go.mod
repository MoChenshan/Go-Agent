module git.woa.com/trpc-go/trpc-agent-go/examples/agui

go 1.24.4

replace (
	git.woa.com/trpc-go/trpc-agent-go => ../../
	git.woa.com/trpc-go/trpc-agent-go/trpc/agui => ../../trpc/agui
	git.woa.com/trpc-go/trpc-agent-go/trpc/telemetry/galileo => ../../trpc/telemetry/galileo
	git.woa.com/trpc-go/trpc-agent-go/trpc/telemetry/zhiyan-llm => ../../trpc/telemetry/zhiyan-llm
)

require (
	git.code.oa.com/trpc-go/trpc-go v0.22.0
	git.woa.com/trpc-go/trpc-agent-go v1.6.1-0.20260311021224-936e8dbcf354
	git.woa.com/trpc-go/trpc-agent-go/trpc/agui v1.6.1-0.20260311021224-936e8dbcf354
	git.woa.com/trpc-go/trpc-agent-go/trpc/telemetry/galileo v1.6.1-0.20260311021224-936e8dbcf354
	git.woa.com/trpc-go/trpc-agent-go/trpc/telemetry/zhiyan-llm v1.6.2-0.20260311021224-936e8dbcf354
	github.com/ag-ui-protocol/ag-ui/sdks/community/go v0.0.0-20260305114736-115a967b66a9
	github.com/google/uuid v1.6.0
	github.com/sirupsen/logrus v1.9.3
	go.opentelemetry.io/otel v1.38.0
	go.opentelemetry.io/otel/trace v1.38.0
	trpc.group/trpc-go/trpc-agent-go v1.9.2-0.20260529112842-4f26702973ef
	trpc.group/trpc-go/trpc-agent-go/server/agui v1.9.1-0.20260529112842-4f26702973ef
	trpc.group/trpc-go/trpc-agent-go/session/postgres v1.9.1-0.20260529112842-4f26702973ef
	trpc.group/trpc-go/trpc-agent-go/session/redis v1.9.1-0.20260529112842-4f26702973ef
)

require (
	cloud.google.com/go v0.123.0 // indirect
	cloud.google.com/go/auth v0.17.0 // indirect
	cloud.google.com/go/compute/metadata v0.9.0 // indirect
	git.code.oa.com/trpc-go/trpc-filter/recovery v0.1.4 // indirect
	git.code.oa.com/trpc-go/trpc-metrics-runtime v0.5.22 // indirect
	git.code.oa.com/trpc-go/trpc-utils v0.2.2 // indirect
	git.woa.com/galileo/eco/go/sdk/base v0.24.1 // indirect
	git.woa.com/galileo/trpc-agent-go-galileo v0.0.4 // indirect
	git.woa.com/galileo/trpc-go-galileo v0.23.1-0.20250925023956-f7cd1ca11a48 // indirect
	git.woa.com/jce/jce v1.2.0 // indirect
	git.woa.com/opentelemetry/opentelemetry-go-ecosystem v0.6.0 // indirect
	git.woa.com/tpstelemetry/cgroups v0.2.3 // indirect
	git.woa.com/tpstelemetry/cgroups/cgroupsv2 v0.2.3 // indirect
	git.woa.com/trpc-go/go_reuseport v1.7.0 // indirect
	git.woa.com/trpc-go/tnet v0.1.2 // indirect
	git.woa.com/trpc-go/trpc-mcp-go v0.0.13 // indirect
	git.woa.com/trpc/trpc-robust/go-sdk v0.0.1 // indirect
	git.woa.com/trpc/trpc-robust/proto/pb/go/trpc-robust v0.0.0-20240820014626-322181997537 // indirect
	git.woa.com/zhiyan-monitor/sdk/llm_go_sdk v0.1.15 // indirect
	github.com/BurntSushi/toml v0.4.1 // indirect
	github.com/alphadose/haxmap v1.4.1 // indirect
	github.com/andybalholm/brotli v1.1.0 // indirect
	github.com/anthropics/anthropic-sdk-go v1.37.0 // indirect
	github.com/bahlo/generic-list-go v0.2.0 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/bmatcuk/doublestar/v4 v4.9.1 // indirect
	github.com/buger/jsonparser v1.1.2 // indirect
	github.com/bytedance/gopkg v0.1.3 // indirect
	github.com/bytedance/sonic v1.14.1 // indirect
	github.com/bytedance/sonic/loader v0.3.0 // indirect
	github.com/cenkalti/backoff/v4 v4.3.0 // indirect
	github.com/cenkalti/backoff/v5 v5.0.3 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/cloudwego/base64x v0.1.6 // indirect
	github.com/creack/pty v1.1.24 // indirect
	github.com/dgryski/go-rendezvous v0.0.0-20200823014737-9f7001d12a5f // indirect
	github.com/dlclark/regexp2 v1.11.4 // indirect
	github.com/felixge/httpsnoop v1.0.4 // indirect
	github.com/fsnotify/fsnotify v1.7.0 // indirect
	github.com/getkin/kin-openapi v0.133.0 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-ole/go-ole v1.3.0 // indirect
	github.com/go-openapi/jsonpointer v0.22.3 // indirect
	github.com/go-openapi/swag/jsonname v0.25.3 // indirect
	github.com/go-playground/form/v4 v4.2.1 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/mock v1.6.0 // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/golang/snappy v0.0.4 // indirect
	github.com/google/flatbuffers v24.3.25+incompatible // indirect
	github.com/google/go-cmp v0.7.0 // indirect
	github.com/google/pprof v0.0.0-20240722153945-304e4f0156b8 // indirect
	github.com/google/s2a-go v0.1.9 // indirect
	github.com/googleapis/enterprise-certificate-proxy v0.3.7 // indirect
	github.com/googleapis/gax-go/v2 v2.15.0 // indirect
	github.com/gorilla/websocket v1.5.3 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.27.3 // indirect
	github.com/guillermo/go.procmeminfo v0.0.0-20131127224636-be4355a9fb0e // indirect
	github.com/hashicorp/errwrap v1.1.0 // indirect
	github.com/hashicorp/go-multierror v1.1.1 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/pgx/v5 v5.7.2 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/jinzhu/copier v0.4.0 // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/jxskiss/base62 v1.1.0 // indirect
	github.com/kelindar/bitmap v1.5.2 // indirect
	github.com/kelindar/simd v1.1.2 // indirect
	github.com/klauspost/compress v1.18.3 // indirect
	github.com/klauspost/cpuid/v2 v2.2.9 // indirect
	github.com/lestrrat-go/strftime v1.0.6 // indirect
	github.com/lufia/plan9stats v0.0.0-20240513124658-fba389f38bae // indirect
	github.com/mailru/easyjson v0.9.1 // indirect
	github.com/mitchellh/mapstructure v1.5.0 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/mohae/deepcopy v0.0.0-20170929034955-c48cc78d4826 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/nanmu42/limitio v1.0.0 // indirect
	github.com/neurosnap/sentences v1.1.2 // indirect
	github.com/oasdiff/yaml v0.0.0-20250309154309-f31be36b4037 // indirect
	github.com/oasdiff/yaml3 v0.0.0-20250309153720-d2182401db90 // indirect
	github.com/ollama/ollama v0.16.3 // indirect
	github.com/openai/openai-go v1.12.0 // indirect
	github.com/panjf2000/ants/v2 v2.11.3 // indirect
	github.com/perimeterx/marshmallow v1.1.5 // indirect
	github.com/pierrec/lz4/v4 v4.1.21 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pkoukk/tiktoken-go v0.1.7 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/power-devops/perfstat v0.0.0-20240221224432-82ca36839d55 // indirect
	github.com/prometheus/client_golang v1.20.5 // indirect
	github.com/prometheus/client_model v0.6.1 // indirect
	github.com/prometheus/common v0.55.0 // indirect
	github.com/prometheus/procfs v0.15.1 // indirect
	github.com/qianbin/directcache v0.9.7 // indirect
	github.com/r3labs/sse/v2 v2.10.0 // indirect
	github.com/redis/go-redis/v9 v9.11.0 // indirect
	github.com/shirou/gopsutil/v3 v3.23.7 // indirect
	github.com/shopspring/decimal v1.3.1 // indirect
	github.com/spaolacci/murmur3 v1.1.0 // indirect
	github.com/spf13/cast v1.6.0 // indirect
	github.com/tidwall/gjson v1.18.0 // indirect
	github.com/tidwall/match v1.2.0 // indirect
	github.com/tidwall/pretty v1.2.1 // indirect
	github.com/tidwall/sjson v1.2.5 // indirect
	github.com/tklauser/go-sysconf v0.3.14 // indirect
	github.com/tklauser/numcpus v0.8.0 // indirect
	github.com/twitchyliquid64/golang-asm v0.15.1 // indirect
	github.com/valyala/bytebufferpool v1.0.0 // indirect
	github.com/valyala/fasthttp v1.52.0 // indirect
	github.com/wk8/go-ordered-map/v2 v2.1.8 // indirect
	github.com/woodsbury/decimal128 v1.4.0 // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	github.com/yusufpapurcu/wmi v1.2.4 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.63.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc v1.28.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp v1.28.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.38.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.38.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp v1.38.0 // indirect
	go.opentelemetry.io/otel/metric v1.38.0 // indirect
	go.opentelemetry.io/otel/sdk v1.38.0 // indirect
	go.opentelemetry.io/otel/sdk/metric v1.38.0 // indirect
	go.opentelemetry.io/proto/otlp v1.9.0 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	go.uber.org/automaxprocs v1.5.4-0.20240213192314-8553d3bb2149 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	go.uber.org/zap v1.27.1 // indirect
	golang.org/x/arch v0.8.0 // indirect
	golang.org/x/crypto v0.45.0 // indirect
	golang.org/x/exp v0.0.0-20250218142911-aa4b98e5adaa // indirect
	golang.org/x/net v0.47.0 // indirect
	golang.org/x/sync v0.18.0 // indirect
	golang.org/x/sys v0.38.0 // indirect
	golang.org/x/text v0.31.0 // indirect
	google.golang.org/genai v1.36.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20251111163417-95abcf5c77ba // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20251124214823-79d6a2a48846 // indirect
	google.golang.org/grpc v1.77.0 // indirect
	google.golang.org/protobuf v1.36.10 // indirect
	gopkg.in/cenkalti/backoff.v1 v1.1.0 // indirect
	gopkg.in/natefinch/lumberjack.v2 v2.2.1 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	trpc.group/trpc-go/trpc-a2a-go v0.2.5 // indirect
	trpc.group/trpc-go/trpc-agent-go/evaluation v1.9.1-0.20260529112842-4f26702973ef // indirect
	trpc.group/trpc-go/trpc-agent-go/model/anthropic v1.9.1-0.20260529112842-4f26702973ef // indirect
	trpc.group/trpc-go/trpc-agent-go/model/gemini v1.9.1-0.20260529112842-4f26702973ef // indirect
	trpc.group/trpc-go/trpc-agent-go/model/ollama v1.9.1-0.20260529112842-4f26702973ef // indirect
	trpc.group/trpc-go/trpc-agent-go/model/provider v1.9.1-0.20260529112842-4f26702973ef // indirect
	trpc.group/trpc-go/trpc-agent-go/storage/postgres v1.9.1-0.20260529112842-4f26702973ef // indirect
	trpc.group/trpc-go/trpc-agent-go/storage/redis v1.9.1-0.20260529112842-4f26702973ef // indirect
	trpc.group/trpc-go/trpc-mcp-go v0.0.14 // indirect
)
