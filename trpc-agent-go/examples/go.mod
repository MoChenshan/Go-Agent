module git.woa.com/trpc-go/trpc-agent-go/examples

go 1.24.6

replace (
	git.woa.com/trpc-go/trpc-agent-go => ../
	git.woa.com/trpc-go/trpc-agent-go/trpc/agent/knot => ../trpc/agent/knot
	git.woa.com/trpc-go/trpc-agent-go/trpc/evaluation => ../trpc/evaluation
	git.woa.com/trpc-go/trpc-agent-go/trpc/promptiter => ../trpc/promptiter
	git.woa.com/trpc-go/trpc-agent-go/trpc/server/debug => ../trpc/server/debug
	git.woa.com/trpc-go/trpc-agent-go/trpc/storage/redis => ../trpc/storage/redis
	git.woa.com/trpc-go/trpc-agent-go/trpc/telemetry/galileo => ../trpc/telemetry/galileo
	git.woa.com/trpc-go/trpc-agent-go/trpc/telemetry/zhiyan => ../trpc/telemetry/zhiyan
	git.woa.com/trpc-go/trpc-agent-go/trpc/telemetry/zhiyan-llm => ../trpc/telemetry/zhiyan-llm
	github.com/redis/go-redis/v9 => github.com/redis/go-redis/v9 v9.16.0
)

require (
	git.code.oa.com/trpc-go/trpc-go v0.22.0
	git.code.oa.com/trpc-go/trpc-naming-polaris v0.5.27
	git.woa.com/galileo/eco/go/sdk/base v0.24.1
	git.woa.com/galileo/trpc-agent-go-galileo v0.0.4
	git.woa.com/trpc-go/trpc-a2a-go v0.2.2
	git.woa.com/trpc-go/trpc-agent-go v1.6.1-0.20260311021224-936e8dbcf354
	git.woa.com/trpc-go/trpc-agent-go/trpc/agent/knot v1.6.1-0.20260311021224-936e8dbcf354
	git.woa.com/trpc-go/trpc-agent-go/trpc/evaluation v1.6.1-0.20260311021224-936e8dbcf354
	git.woa.com/trpc-go/trpc-agent-go/trpc/promptiter v1.6.1-0.20260311021224-936e8dbcf354
	git.woa.com/trpc-go/trpc-agent-go/trpc/server/debug v1.6.1-0.20260311021224-936e8dbcf354
	git.woa.com/trpc-go/trpc-agent-go/trpc/storage/redis v1.6.1-0.20260311021224-936e8dbcf354
	git.woa.com/trpc-go/trpc-agent-go/trpc/telemetry/galileo v1.6.1-0.20260311021224-936e8dbcf354
	git.woa.com/trpc-go/trpc-agent-go/trpc/telemetry/zhiyan v1.6.1-0.20260311021224-936e8dbcf354
	git.woa.com/trpc-go/trpc-agent-go/trpc/telemetry/zhiyan-llm v1.6.2-0.20260311021224-936e8dbcf354
	github.com/asg017/sqlite-vec-go-bindings v0.1.6
	github.com/cloudernative/dify-sdk-go v1.0.2
	github.com/go-openapi/testify/v2 v2.0.2
	github.com/google/uuid v1.6.0
	github.com/gorilla/mux v1.8.1
	github.com/ledongthuc/pdf v0.0.0-20250511090121-5959a4027728
	github.com/mattn/go-sqlite3 v1.14.32
	github.com/ncruces/go-sqlite3 v0.32.0
	github.com/ollama/ollama v0.16.3
	github.com/openai/openai-go v1.12.0
	github.com/stretchr/testify v1.11.1
	github.com/xuri/excelize/v2 v2.10.0
	github.com/yanyiwu/gojieba v1.4.7
	go.opentelemetry.io/otel v1.41.0
	go.opentelemetry.io/otel/metric v1.41.0
	go.opentelemetry.io/otel/trace v1.41.0
	go.uber.org/zap v1.27.1
	trpc.group/trpc-go/trpc-a2a-go v0.2.5
	trpc.group/trpc-go/trpc-agent-go v1.9.2-0.20260529112842-4f26702973ef
	trpc.group/trpc-go/trpc-agent-go/agent/dify v1.9.1-0.20260529112842-4f26702973ef
	trpc.group/trpc-go/trpc-agent-go/agent/extension/toolpipe v0.0.0-20260529112842-4f26702973ef
	trpc.group/trpc-go/trpc-agent-go/agent/n8n v1.9.1-0.20260529112842-4f26702973ef
	trpc.group/trpc-go/trpc-agent-go/agent/weknora v1.9.1-0.20260529112842-4f26702973ef
	trpc.group/trpc-go/trpc-agent-go/codeexecutor/container v1.9.1-0.20260529112842-4f26702973ef
	trpc.group/trpc-go/trpc-agent-go/codeexecutor/jupyter v1.9.1-0.20260529112842-4f26702973ef
	trpc.group/trpc-go/trpc-agent-go/evaluation v1.9.1-0.20260529112842-4f26702973ef
	trpc.group/trpc-go/trpc-agent-go/graph/checkpoint/redis v1.9.1-0.20260529112842-4f26702973ef
	trpc.group/trpc-go/trpc-agent-go/memory/mysql v1.9.1-0.20260529112842-4f26702973ef
	trpc.group/trpc-go/trpc-agent-go/memory/mysqlvec v1.9.1-0.20260529112842-4f26702973ef
	trpc.group/trpc-go/trpc-agent-go/memory/pgvector v1.9.1-0.20260529112842-4f26702973ef
	trpc.group/trpc-go/trpc-agent-go/memory/postgres v1.9.1-0.20260529112842-4f26702973ef
	trpc.group/trpc-go/trpc-agent-go/memory/redis v1.9.1-0.20260529112842-4f26702973ef
	trpc.group/trpc-go/trpc-agent-go/memory/sqlite v1.9.1-0.20260529112842-4f26702973ef
	trpc.group/trpc-go/trpc-agent-go/memory/sqlitevec v1.9.1-0.20260529112842-4f26702973ef
	trpc.group/trpc-go/trpc-agent-go/model/anthropic v1.9.1-0.20260529112842-4f26702973ef
	trpc.group/trpc-go/trpc-agent-go/model/provider v1.9.1-0.20260529112842-4f26702973ef
	trpc.group/trpc-go/trpc-agent-go/model/tiktoken v1.9.1-0.20260529112842-4f26702973ef
	trpc.group/trpc-go/trpc-agent-go/server/agui v1.9.1-0.20260529112842-4f26702973ef
	trpc.group/trpc-go/trpc-agent-go/server/evaluation v1.9.1-0.20260529112842-4f26702973ef
	trpc.group/trpc-go/trpc-agent-go/server/promptiter v1.9.1-0.20260529112842-4f26702973ef
	trpc.group/trpc-go/trpc-agent-go/session/clickhouse v1.9.1-0.20260529112842-4f26702973ef
	trpc.group/trpc-go/trpc-agent-go/session/mysql v1.9.1-0.20260529112842-4f26702973ef
	trpc.group/trpc-go/trpc-agent-go/session/pgvector v1.9.1-0.20260529112842-4f26702973ef
	trpc.group/trpc-go/trpc-agent-go/session/postgres v1.9.1-0.20260529112842-4f26702973ef
	trpc.group/trpc-go/trpc-agent-go/session/redis v1.9.1-0.20260529112842-4f26702973ef
	trpc.group/trpc-go/trpc-agent-go/session/sqlite v1.9.1-0.20260529112842-4f26702973ef
	trpc.group/trpc-go/trpc-agent-go/tool/arxivsearch v1.9.1-0.20260529112842-4f26702973ef
	trpc.group/trpc-go/trpc-agent-go/tool/email v1.9.1-0.20260529112842-4f26702973ef
	trpc.group/trpc-go/trpc-agent-go/tool/google v1.9.1-0.20260529112842-4f26702973ef
	trpc.group/trpc-go/trpc-agent-go/tool/openapi v1.9.1-0.20260529112842-4f26702973ef
	trpc.group/trpc-go/trpc-agent-go/tool/webfetch/geminifetch v1.9.1-0.20260529112842-4f26702973ef
	trpc.group/trpc-go/trpc-agent-go/tool/webfetch/httpfetch v1.9.1-0.20260529112842-4f26702973ef
	trpc.group/trpc-go/trpc-agent-go/tool/wikipedia v1.9.1-0.20260529112842-4f26702973ef
	trpc.group/trpc-go/trpc-mcp-go v0.0.14
)

require (
	cloud.google.com/go v0.123.0 // indirect
	cloud.google.com/go/auth v0.17.0 // indirect
	cloud.google.com/go/auth/oauth2adapt v0.2.8 // indirect
	cloud.google.com/go/compute/metadata v0.9.0 // indirect
	filippo.io/edwards25519 v1.1.0 // indirect
	github.com/ClickHouse/ch-go v0.61.5 // indirect
	github.com/ClickHouse/clickhouse-go/v2 v2.26.0 // indirect
	github.com/JohannesKaufmann/dom v0.2.0 // indirect
	github.com/JohannesKaufmann/html-to-markdown/v2 v2.5.0 // indirect
	github.com/Microsoft/go-winio v0.6.2 // indirect
	github.com/Tencent/WeKnora/client v0.0.0-20260324035655-62e6ae960f46 // indirect
	github.com/ag-ui-protocol/ag-ui/sdks/community/go v0.0.0-20260305114736-115a967b66a9 // indirect
	github.com/bahlo/generic-list-go v0.2.0 // indirect
	github.com/bufbuild/protocompile v0.14.1 // indirect
	github.com/buger/jsonparser v1.1.2 // indirect
	github.com/cenkalti/backoff/v5 v5.0.3 // indirect
	github.com/clipperhouse/uax29/v2 v2.2.0 // indirect
	github.com/containerd/errdefs v1.0.0 // indirect
	github.com/containerd/errdefs/pkg v0.3.0 // indirect
	github.com/containerd/log v0.1.0 // indirect
	github.com/creack/pty v1.1.24 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/distribution/reference v0.6.0 // indirect
	github.com/docker/docker v28.4.0+incompatible // indirect
	github.com/docker/go-connections v0.5.0 // indirect
	github.com/docker/go-units v0.5.0 // indirect
	github.com/felixge/httpsnoop v1.0.4 // indirect
	github.com/go-ego/gse v1.0.0 // indirect
	github.com/go-faster/city v1.0.1 // indirect
	github.com/go-faster/errors v0.7.1 // indirect
	github.com/go-sql-driver/mysql v1.9.3 // indirect
	github.com/google/btree v1.1.2 // indirect
	github.com/google/go-cmp v0.7.0 // indirect
	github.com/google/s2a-go v0.1.9 // indirect
	github.com/googleapis/enterprise-certificate-proxy v0.3.7 // indirect
	github.com/googleapis/gax-go/v2 v2.15.0 // indirect
	github.com/gorilla/websocket v1.5.3 // indirect
	github.com/hhrutter/lzw v1.0.0 // indirect
	github.com/hhrutter/pkcs7 v0.2.0 // indirect
	github.com/hhrutter/tiff v1.0.2 // indirect
	github.com/itchyny/gojq v0.12.16 // indirect
	github.com/itchyny/timefmt-go v0.1.6 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/pgx/v5 v5.7.6 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/lib/pq v1.10.9 // indirect
	github.com/mattn/go-runewidth v0.0.19 // indirect
	github.com/moby/docker-image-spec v1.3.1 // indirect
	github.com/moby/go-archive v0.1.0 // indirect
	github.com/moby/patternmatcher v0.6.0 // indirect
	github.com/moby/sys/sequential v0.6.0 // indirect
	github.com/moby/sys/user v0.4.0 // indirect
	github.com/moby/sys/userns v0.1.0 // indirect
	github.com/ncruces/julianday v1.0.0 // indirect
	github.com/neurosnap/sentences v1.1.2 // indirect
	github.com/opencontainers/go-digest v1.0.0 // indirect
	github.com/opencontainers/image-spec v1.1.0-rc5 // indirect
	github.com/paulmach/orb v0.11.1 // indirect
	github.com/pdfcpu/pdfcpu v0.11.1 // indirect
	github.com/pgvector/pgvector-go v0.2.3 // indirect
	github.com/richardlehane/mscfb v1.0.4 // indirect
	github.com/richardlehane/msoleps v1.0.4 // indirect
	github.com/sirupsen/logrus v1.9.3 // indirect
	github.com/tetratelabs/wazero v1.11.0 // indirect
	github.com/tiendc/go-deepcopy v1.7.1 // indirect
	github.com/vcaesar/cedar v0.20.2 // indirect
	github.com/wk8/go-ordered-map/v2 v2.1.8 // indirect
	github.com/wneessen/go-mail v0.7.2 // indirect
	github.com/xuri/efp v0.0.1 // indirect
	github.com/xuri/nfp v0.0.2-0.20250530014748-2ddeb826f9a9 // indirect
	github.com/yuin/goldmark v1.7.13 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.63.0 // indirect
	golang.org/x/image v0.32.0 // indirect
	google.golang.org/api v0.256.0 // indirect
	google.golang.org/genai v1.36.0 // indirect
	mvdan.cc/sh/v3 v3.8.0 // indirect
	sigs.k8s.io/yaml v1.4.0 // indirect
	trpc.group/trpc-go/trpc-agent-go/knowledge/document/reader/pdf v1.9.1-0.20260529112842-4f26702973ef // indirect
	trpc.group/trpc-go/trpc-agent-go/model/gemini v1.9.1-0.20260529112842-4f26702973ef // indirect
	trpc.group/trpc-go/trpc-agent-go/model/ollama v1.9.1-0.20260529112842-4f26702973ef // indirect
	trpc.group/trpc-go/trpc-agent-go/storage/clickhouse v1.9.1-0.20260529112842-4f26702973ef // indirect
	trpc.group/trpc-go/trpc-agent-go/storage/mysql v1.9.1-0.20260529112842-4f26702973ef // indirect
	trpc.group/trpc-go/trpc-agent-go/storage/postgres v1.9.1-0.20260529112842-4f26702973ef // indirect
	trpc.group/trpc-go/trpc-agent-go/storage/redis v1.9.1-0.20260529112842-4f26702973ef // indirect
)

require (
	git.code.oa.com/polaris/polaris-go v0.12.12 // indirect
	git.code.oa.com/trpc-go/trpc-filter/recovery v0.1.4 // indirect
	git.code.oa.com/trpc-go/trpc-metrics-runtime v0.5.22 // indirect
	git.code.oa.com/trpc-go/trpc-utils v0.2.2 // indirect
	git.woa.com/galileo/trpc-go-galileo v0.23.1-0.20250925023956-f7cd1ca11a48 // indirect
	git.woa.com/jce/jce v1.2.0 // indirect
	git.woa.com/opentelemetry/opentelemetry-go-ecosystem v0.6.3 // indirect
	git.woa.com/opentelemetry/opentelemetry-go-ecosystem/instrumentation/oteltrpc v0.6.3 // indirect
	git.woa.com/polaris/polaris-go/v2 v2.6.7 // indirect
	git.woa.com/polaris/polaris-server-api/api/metric v1.0.0 // indirect
	git.woa.com/polaris/polaris-server-api/api/monitor v1.0.7 // indirect
	git.woa.com/polaris/polaris-server-api/api/v1/grpc v1.0.2 // indirect
	git.woa.com/polaris/polaris-server-api/api/v1/model v1.1.4 // indirect
	git.woa.com/polaris/polaris-server-api/api/v2/grpc v1.0.0 // indirect
	git.woa.com/polaris/polaris-server-api/api/v2/model v1.0.3 // indirect
	git.woa.com/tpstelemetry/cgroups v0.2.3 // indirect
	git.woa.com/tpstelemetry/cgroups/cgroupsv2 v0.2.3 // indirect
	git.woa.com/tpstelemetry/tpstelemetry-protocol v0.0.2-0.20230403124315-f383964b6bcc // indirect
	git.woa.com/trpc-go/go_reuseport v1.7.0 // indirect
	git.woa.com/trpc-go/tnet v0.1.3-0.20251204063419-b13cf17778b9 // indirect
	git.woa.com/trpc-go/trpc-database/goredis/v3 v3.3.8 // indirect
	git.woa.com/trpc-go/trpc-mcp-go v0.0.13 // indirect
	git.woa.com/trpc/trpc-robust/go-sdk v0.0.1 // indirect
	git.woa.com/trpc/trpc-robust/proto/pb/go/trpc-robust v0.0.0-20240820014626-322181997537 // indirect
	git.woa.com/zhiyan-monitor/sdk/llm_go_sdk v0.1.15 // indirect
	github.com/BurntSushi/toml v0.4.1 // indirect
	github.com/alphadose/haxmap v1.4.1 // indirect
	github.com/andybalholm/brotli v1.1.0 // indirect
	github.com/anthropics/anthropic-sdk-go v1.37.0
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/bmatcuk/doublestar/v4 v4.9.1 // indirect
	github.com/bytedance/gopkg v0.1.3 // indirect
	github.com/bytedance/sonic v1.14.1 // indirect
	github.com/bytedance/sonic/loader v0.3.0 // indirect
	github.com/cenkalti/backoff/v4 v4.3.0 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/cloudwego/base64x v0.1.6 // indirect
	github.com/coreos/go-semver v0.3.0 // indirect
	github.com/coreos/go-systemd/v22 v22.3.2 // indirect
	github.com/decred/dcrd/dcrec/secp256k1/v4 v4.4.0 // indirect
	github.com/dgryski/go-rendezvous v0.0.0-20200823014737-9f7001d12a5f // indirect
	github.com/dlclark/regexp2 v1.11.5 // indirect
	github.com/fsnotify/fsnotify v1.7.0 // indirect
	github.com/getkin/kin-openapi v0.133.0
	github.com/ghodss/yaml v1.0.0 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-ole/go-ole v1.3.0 // indirect
	github.com/go-openapi/jsonpointer v0.22.3 // indirect
	github.com/go-openapi/swag/jsonname v0.25.3 // indirect
	github.com/go-playground/form/v4 v4.2.1 // indirect
	github.com/goccy/go-json v0.10.3 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang-jwt/jwt/v5 v5.2.3 // indirect
	github.com/golang/mock v1.6.0 // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/golang/snappy v0.0.4 // indirect
	github.com/google/flatbuffers v24.3.25+incompatible // indirect
	github.com/google/pprof v0.0.0-20240722153945-304e4f0156b8 // indirect
	github.com/grpc-ecosystem/go-grpc-prometheus v1.2.0 // indirect
	github.com/grpc-ecosystem/grpc-gateway v1.16.0 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.27.3 // indirect
	github.com/guillermo/go.procmeminfo v0.0.0-20131127224636-be4355a9fb0e // indirect
	github.com/hashicorp/errwrap v1.1.0 // indirect
	github.com/hashicorp/go-multierror v1.1.1 // indirect
	github.com/jinzhu/copier v0.4.0 // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/jxskiss/base62 v1.1.0 // indirect
	github.com/kelindar/bitmap v1.5.2 // indirect
	github.com/kelindar/simd v1.1.2 // indirect
	github.com/klauspost/compress v1.18.3 // indirect
	github.com/klauspost/cpuid/v2 v2.2.9 // indirect
	github.com/lestrrat-go/blackmagic v1.0.2 // indirect
	github.com/lestrrat-go/httpcc v1.0.1 // indirect
	github.com/lestrrat-go/httprc v1.0.6 // indirect
	github.com/lestrrat-go/iter v1.0.2 // indirect
	github.com/lestrrat-go/jwx/v2 v2.1.4 // indirect
	github.com/lestrrat-go/option v1.0.1 // indirect
	github.com/lestrrat-go/strftime v1.0.6 // indirect
	github.com/lufia/plan9stats v0.0.0-20240513124658-fba389f38bae // indirect
	github.com/mailru/easyjson v0.9.1 // indirect
	github.com/mitchellh/go-homedir v1.1.0 // indirect
	github.com/mitchellh/mapstructure v1.5.0 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/mohae/deepcopy v0.0.0-20170929034955-c48cc78d4826 // indirect
	github.com/mozillazg/go-pinyin v0.18.0 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/nanmu42/limitio v1.0.0 // indirect
	github.com/natefinch/lumberjack v2.0.0+incompatible // indirect
	github.com/oasdiff/yaml v0.0.0-20250309154309-f31be36b4037 // indirect
	github.com/oasdiff/yaml3 v0.0.0-20250309153720-d2182401db90 // indirect
	github.com/panjf2000/ants/v2 v2.11.3 // indirect
	github.com/perimeterx/marshmallow v1.1.5 // indirect
	github.com/pierrec/lz4/v4 v4.1.21 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pkoukk/tiktoken-go v0.1.7 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/power-devops/perfstat v0.0.0-20240221224432-82ca36839d55 // indirect
	github.com/prometheus/client_golang v1.20.5 // indirect
	github.com/prometheus/client_model v0.6.1 // indirect
	github.com/prometheus/common v0.55.0 // indirect
	github.com/prometheus/procfs v0.15.1 // indirect
	github.com/qianbin/directcache v0.9.7 // indirect
	github.com/r3labs/sse/v2 v2.10.0 // indirect
	github.com/redis/go-redis/v9 v9.17.0 // indirect
	github.com/rs/cors v1.11.1 // indirect
	github.com/segmentio/asm v1.2.0 // indirect
	github.com/shirou/gopsutil/v3 v3.23.12 // indirect
	github.com/shirou/gopsutil/v4 v4.24.6 // indirect
	github.com/shoenig/go-m1cpu v0.1.6 // indirect
	github.com/shopspring/decimal v1.4.0 // indirect
	github.com/spaolacci/murmur3 v1.1.0 // indirect
	github.com/spf13/cast v1.6.0 // indirect
	github.com/tidwall/gjson v1.18.0 // indirect
	github.com/tidwall/match v1.2.0 // indirect
	github.com/tidwall/pretty v1.2.1 // indirect
	github.com/tidwall/sjson v1.2.5 // indirect
	github.com/tiktoken-go/tokenizer v0.7.0 // indirect
	github.com/tklauser/go-sysconf v0.3.14 // indirect
	github.com/tklauser/numcpus v0.8.0 // indirect
	github.com/twitchyliquid64/golang-asm v0.15.1 // indirect
	github.com/valyala/bytebufferpool v1.0.0 // indirect
	github.com/valyala/fasthttp v1.52.0 // indirect
	github.com/woodsbury/decimal128 v1.4.0 // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	github.com/yusufpapurcu/wmi v1.2.4 // indirect
	go.etcd.io/etcd/api/v3 v3.5.9 // indirect
	go.etcd.io/etcd/client/pkg/v3 v3.5.9 // indirect
	go.etcd.io/etcd/client/v3 v3.5.9 // indirect
	go.opentelemetry.io/contrib/instrumentation/host v0.53.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/runtime v0.53.0 // indirect
	go.opentelemetry.io/contrib/zpages v0.53.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc v1.28.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp v1.28.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.38.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.38.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp v1.38.0 // indirect
	go.opentelemetry.io/otel/sdk v1.41.0 // indirect
	go.opentelemetry.io/otel/sdk/metric v1.41.0 // indirect
	go.opentelemetry.io/proto/otlp v1.9.0 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	go.uber.org/automaxprocs v1.6.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	golang.org/x/arch v0.8.0 // indirect
	golang.org/x/crypto v0.48.0 // indirect
	golang.org/x/exp v0.0.0-20250218142911-aa4b98e5adaa // indirect
	golang.org/x/net v0.49.0 // indirect
	golang.org/x/oauth2 v0.33.0 // indirect
	golang.org/x/sync v0.19.0 // indirect
	golang.org/x/sys v0.41.0 // indirect
	golang.org/x/text v0.34.0 // indirect
	google.golang.org/genproto v0.0.0-20250603155806-513f23925822 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20251111163417-95abcf5c77ba // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20251124214823-79d6a2a48846 // indirect
	google.golang.org/grpc v1.77.0 // indirect
	google.golang.org/protobuf v1.36.10 // indirect
	gopkg.in/cenkalti/backoff.v1 v1.1.0 // indirect
	gopkg.in/natefinch/lumberjack.v2 v2.2.1 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
