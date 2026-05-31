module git.woa.com/trpc-go/trpc-agent-go/examples/knowledge

go 1.24.6

toolchain go1.24.11

replace (
	git.woa.com/trpc-go/trpc-agent-go => ../../
	git.woa.com/trpc-go/trpc-agent-go/trpc/storage/goes => ../../trpc/storage/goes
	git.woa.com/trpc-go/trpc-agent-go/trpc/storage/postgres => ../../trpc/storage/postgres
	git.woa.com/trpc-go/trpc-agent-go/trpc/storage/tcvector => ../../trpc/storage/tcvector
	go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc => go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc v0.46.0
)

require (
	git.code.oa.com/trpc-go/trpc-go v0.21.1
	git.code.oa.com/trpc-go/trpc-naming-polaris v0.5.28
	git.woa.com/trag/trag-sdk/go-trag v0.0.0-20250916081403-04014d9b5330
	git.woa.com/trpc-go/trpc-agent-go v1.6.1-0.20260311021224-936e8dbcf354
	git.woa.com/trpc-go/trpc-agent-go/trpc/storage/goes v1.6.1-0.20260311021224-936e8dbcf354
	git.woa.com/trpc-go/trpc-agent-go/trpc/storage/postgres v1.6.1-0.20260311021224-936e8dbcf354
	git.woa.com/trpc-go/trpc-agent-go/trpc/storage/tcvector v1.6.1-0.20260311021224-936e8dbcf354
	github.com/getkin/kin-openapi v0.133.0
	github.com/google/uuid v1.6.0
	github.com/tencent/vectordatabase-sdk-go v1.8.4
	trpc.group/trpc-go/trpc-agent-go v1.9.2-0.20260529112842-4f26702973ef
	trpc.group/trpc-go/trpc-agent-go/knowledge/document/reader/pdf v1.9.1-0.20260529112842-4f26702973ef
	trpc.group/trpc-go/trpc-agent-go/knowledge/embedder/gemini v1.9.1-0.20260529112842-4f26702973ef
	trpc.group/trpc-go/trpc-agent-go/knowledge/embedder/huggingface v1.9.1-0.20260529112842-4f26702973ef
	trpc.group/trpc-go/trpc-agent-go/knowledge/embedder/ollama v1.9.1-0.20260529112842-4f26702973ef
	trpc.group/trpc-go/trpc-agent-go/knowledge/ocr/tesseract v1.9.1-0.20260529112842-4f26702973ef
	trpc.group/trpc-go/trpc-agent-go/knowledge/vectorstore/elasticsearch v1.9.1-0.20260529112842-4f26702973ef
	trpc.group/trpc-go/trpc-agent-go/knowledge/vectorstore/milvus v1.9.1-0.20260529112842-4f26702973ef
	trpc.group/trpc-go/trpc-agent-go/knowledge/vectorstore/pgvector v1.9.1-0.20260529112842-4f26702973ef
	trpc.group/trpc-go/trpc-agent-go/knowledge/vectorstore/sqlitevec v1.9.1-0.20260529112842-4f26702973ef
	trpc.group/trpc-go/trpc-agent-go/knowledge/vectorstore/tcvector v1.9.1-0.20260529112842-4f26702973ef
	trpc.group/trpc-go/trpc-agent-go/storage/elasticsearch v1.9.1-0.20260529112842-4f26702973ef
	trpc.group/trpc-go/trpc-mcp-go v0.0.14
)

require (
	cloud.google.com/go v0.123.0 // indirect
	cloud.google.com/go/auth v0.17.0 // indirect
	cloud.google.com/go/compute/metadata v0.9.0 // indirect
	git.code.oa.com/polaris/polaris-go v0.12.12 // indirect
	git.code.oa.com/trpc-go/trpc-database/postgres v0.4.0 // indirect
	git.code.oa.com/trpc-go/trpc-metrics-runtime v0.5.22 // indirect
	git.code.oa.com/trpc-go/trpc-selector-dsn v0.2.1 // indirect
	git.code.oa.com/trpc-go/trpc-utils v0.2.2 // indirect
	git.woa.com/jce/jce v1.2.0 // indirect
	git.woa.com/polaris/polaris-server-api/api/metric v1.0.0 // indirect
	git.woa.com/polaris/polaris-server-api/api/monitor v1.0.7 // indirect
	git.woa.com/polaris/polaris-server-api/api/v1/grpc v1.0.2 // indirect
	git.woa.com/polaris/polaris-server-api/api/v1/model v1.1.4 // indirect
	git.woa.com/polaris/polaris-server-api/api/v2/grpc v1.0.0 // indirect
	git.woa.com/polaris/polaris-server-api/api/v2/model v1.0.3 // indirect
	git.woa.com/trpc-go/go_reuseport v1.7.0 // indirect
	git.woa.com/trpc-go/tnet v0.1.3-0.20251204063419-b13cf17778b9 // indirect
	git.woa.com/trpc-go/trpc-database/goes v0.0.8 // indirect
	git.woa.com/trpc-go/trpc-database/tcvectordb v0.1.3 // indirect
	git.woa.com/trpc-go/trpc-mcp-go v0.0.13 // indirect
	git.woa.com/trpc/trpc-robust/go-sdk v0.0.1 // indirect
	git.woa.com/trpc/trpc-robust/proto/pb/go/trpc-robust v0.0.0-20240820014626-322181997537 // indirect
	github.com/BurntSushi/toml v0.3.1 // indirect
	github.com/andybalholm/brotli v1.1.0 // indirect
	github.com/asg017/sqlite-vec-go-bindings v0.1.6 // indirect
	github.com/bahlo/generic-list-go v0.2.0 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/blang/semver/v4 v4.0.0 // indirect
	github.com/bmatcuk/doublestar/v4 v4.9.1 // indirect
	github.com/bufbuild/protocompile v0.14.1 // indirect
	github.com/buger/jsonparser v1.1.1 // indirect
	github.com/cenkalti/backoff/v4 v4.3.0 // indirect
	github.com/cenkalti/backoff/v5 v5.0.3 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/cilium/ebpf v0.11.0 // indirect
	github.com/clbanning/mxj v1.8.4 // indirect
	github.com/clipperhouse/uax29/v2 v2.2.0 // indirect
	github.com/cockroachdb/errors v1.9.1 // indirect
	github.com/cockroachdb/logtags v0.0.0-20211118104740-dabe8e521a4f // indirect
	github.com/cockroachdb/redact v1.1.3 // indirect
	github.com/containerd/cgroups/v3 v3.0.3 // indirect
	github.com/coreos/go-semver v0.3.0 // indirect
	github.com/coreos/go-systemd/v22 v22.3.2 // indirect
	github.com/creack/pty v1.1.24 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/docker/go-units v0.5.0 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/elastic/elastic-transport-go/v8 v8.8.0 // indirect
	github.com/elastic/go-elasticsearch/v7 v7.17.10 // indirect
	github.com/elastic/go-elasticsearch/v8 v8.19.0 // indirect
	github.com/elastic/go-elasticsearch/v9 v9.2.0 // indirect
	github.com/felixge/httpsnoop v1.0.4 // indirect
	github.com/fsnotify/fsnotify v1.7.0 // indirect
	github.com/fxamacker/cbor/v2 v2.7.0 // indirect
	github.com/getsentry/sentry-go v0.12.0 // indirect
	github.com/ghodss/yaml v1.0.0 // indirect
	github.com/go-ego/gse v1.0.0 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-ole/go-ole v1.2.6 // indirect
	github.com/go-openapi/jsonpointer v0.22.3 // indirect
	github.com/go-openapi/swag/jsonname v0.25.3 // indirect
	github.com/go-playground/form/v4 v4.2.1 // indirect
	github.com/go-sql-driver/mysql v1.5.0 // indirect
	github.com/godbus/dbus/v5 v5.0.4 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang-jwt/jwt/v4 v4.4.2 // indirect
	github.com/golang/mock v1.6.0 // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/golang/snappy v0.0.4 // indirect
	github.com/google/btree v1.1.2 // indirect
	github.com/google/flatbuffers v24.3.25+incompatible // indirect
	github.com/google/go-cmp v0.7.0 // indirect
	github.com/google/go-querystring v1.1.0 // indirect
	github.com/google/s2a-go v0.1.9 // indirect
	github.com/googleapis/enterprise-certificate-proxy v0.3.7 // indirect
	github.com/googleapis/gax-go/v2 v2.15.0 // indirect
	github.com/gorilla/websocket v1.5.3 // indirect
	github.com/grpc-ecosystem/go-grpc-middleware v1.3.0 // indirect
	github.com/grpc-ecosystem/go-grpc-prometheus v1.2.0 // indirect
	github.com/grpc-ecosystem/grpc-gateway v1.16.0 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.27.3 // indirect
	github.com/hashicorp/errwrap v1.1.0 // indirect
	github.com/hashicorp/go-multierror v1.1.1 // indirect
	github.com/hhrutter/lzw v1.0.0 // indirect
	github.com/hhrutter/pkcs7 v0.2.0 // indirect
	github.com/hhrutter/tiff v1.0.2 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/pgx/v5 v5.7.6 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/jinzhu/copier v0.4.0 // indirect
	github.com/jonboulle/clockwork v0.2.2 // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/klauspost/compress v1.18.3 // indirect
	github.com/kr/pretty v0.3.1 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/ledongthuc/pdf v0.0.0-20250511090121-5959a4027728 // indirect
	github.com/lestrrat-go/strftime v1.0.6 // indirect
	github.com/lib/pq v1.10.9 // indirect
	github.com/lufia/plan9stats v0.0.0-20211012122336-39d0f177ccd0 // indirect
	github.com/mailru/easyjson v0.9.1 // indirect
	github.com/mattn/go-runewidth v0.0.19 // indirect
	github.com/mattn/go-sqlite3 v1.14.32 // indirect
	github.com/milvus-io/milvus-proto/go-api/v2 v2.6.3 // indirect
	github.com/milvus-io/milvus/client/v2 v2.6.1 // indirect
	github.com/milvus-io/milvus/pkg/v2 v2.6.3 // indirect
	github.com/mitchellh/go-homedir v1.1.0 // indirect
	github.com/mitchellh/mapstructure v1.5.0 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/mohae/deepcopy v0.0.0-20170929034955-c48cc78d4826 // indirect
	github.com/mozillazg/go-httpheader v0.4.0 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/natefinch/lumberjack v2.0.0+incompatible // indirect
	github.com/ncruces/go-sqlite3 v0.17.1 // indirect
	github.com/ncruces/julianday v1.0.0 // indirect
	github.com/oasdiff/yaml v0.0.0-20250309154309-f31be36b4037 // indirect
	github.com/oasdiff/yaml3 v0.0.0-20250309153720-d2182401db90 // indirect
	github.com/ollama/ollama v0.16.3 // indirect
	github.com/openai/openai-go v1.12.0 // indirect
	github.com/opencontainers/runtime-spec v1.0.2 // indirect
	github.com/otiai10/gosseract/v2 v2.4.1 // indirect
	github.com/panjf2000/ants/v2 v2.11.3 // indirect
	github.com/pdfcpu/pdfcpu v0.11.1 // indirect
	github.com/perimeterx/marshmallow v1.1.5 // indirect
	github.com/pgvector/pgvector-go v0.3.0 // indirect
	github.com/pierrec/lz4/v4 v4.1.22 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/power-devops/perfstat v0.0.0-20210106213030-5aafc221ea8c // indirect
	github.com/prometheus/client_golang v1.20.5 // indirect
	github.com/prometheus/client_model v0.6.1 // indirect
	github.com/prometheus/common v0.55.0 // indirect
	github.com/prometheus/procfs v0.15.1 // indirect
	github.com/r3labs/sse/v2 v2.10.0 // indirect
	github.com/rogpeppe/go-internal v1.14.1 // indirect
	github.com/samber/lo v1.27.0 // indirect
	github.com/shirou/gopsutil/v3 v3.23.12 // indirect
	github.com/shoenig/go-m1cpu v0.1.6 // indirect
	github.com/sirupsen/logrus v1.9.3 // indirect
	github.com/soheilhy/cmux v0.1.5 // indirect
	github.com/spaolacci/murmur3 v1.1.0 // indirect
	github.com/spf13/cast v1.6.0 // indirect
	github.com/spf13/pflag v1.0.5 // indirect
	github.com/stretchr/testify v1.11.1 // indirect
	github.com/tencentyun/cos-go-sdk-v5 v0.7.71 // indirect
	github.com/tetratelabs/wazero v1.7.3 // indirect
	github.com/tidwall/gjson v1.18.0 // indirect
	github.com/tidwall/match v1.2.0 // indirect
	github.com/tidwall/pretty v1.2.1 // indirect
	github.com/tidwall/sjson v1.2.5 // indirect
	github.com/tklauser/go-sysconf v0.3.12 // indirect
	github.com/tklauser/numcpus v0.6.1 // indirect
	github.com/tmc/grpc-websocket-proxy v0.0.0-20201229170055-e5319fda7802 // indirect
	github.com/uber/jaeger-client-go v2.30.0+incompatible // indirect
	github.com/valyala/bytebufferpool v1.0.0 // indirect
	github.com/valyala/fasthttp v1.52.0 // indirect
	github.com/vcaesar/cedar v0.20.2 // indirect
	github.com/wk8/go-ordered-map/v2 v2.1.8 // indirect
	github.com/woodsbury/decimal128 v1.4.0 // indirect
	github.com/x448/float16 v0.8.4 // indirect
	github.com/xiang90/probing v0.0.0-20190116061207-43a291ad63a2 // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	github.com/yuin/goldmark v1.7.13 // indirect
	github.com/yusufpapurcu/wmi v1.2.3 // indirect
	go.etcd.io/bbolt v1.3.11 // indirect
	go.etcd.io/etcd/api/v3 v3.5.17 // indirect
	go.etcd.io/etcd/client/pkg/v3 v3.5.17 // indirect
	go.etcd.io/etcd/client/v2 v2.305.17 // indirect
	go.etcd.io/etcd/client/v3 v3.5.17 // indirect
	go.etcd.io/etcd/pkg/v3 v3.5.17 // indirect
	go.etcd.io/etcd/raft/v3 v3.5.17 // indirect
	go.etcd.io/etcd/server/v3 v3.5.17 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc v0.61.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.63.0 // indirect
	go.opentelemetry.io/otel v1.38.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.38.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.38.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp v1.38.0 // indirect
	go.opentelemetry.io/otel/metric v1.38.0 // indirect
	go.opentelemetry.io/otel/sdk v1.38.0 // indirect
	go.opentelemetry.io/otel/trace v1.38.0 // indirect
	go.opentelemetry.io/proto/otlp v1.9.0 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	go.uber.org/automaxprocs v1.6.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	go.uber.org/zap v1.27.1 // indirect
	golang.org/x/crypto v0.45.0 // indirect
	golang.org/x/exp v0.0.0-20250218142911-aa4b98e5adaa // indirect
	golang.org/x/image v0.32.0 // indirect
	golang.org/x/net v0.47.0 // indirect
	golang.org/x/sync v0.18.0 // indirect
	golang.org/x/sys v0.38.0 // indirect
	golang.org/x/text v0.31.0 // indirect
	golang.org/x/time v0.12.0 // indirect
	google.golang.org/genai v1.36.0 // indirect
	google.golang.org/genproto v0.0.0-20250603155806-513f23925822 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20251111163417-95abcf5c77ba // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20251111163417-95abcf5c77ba // indirect
	google.golang.org/grpc v1.77.0 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
	gopkg.in/cenkalti/backoff.v1 v1.1.0 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	gopkg.in/natefinch/lumberjack.v2 v2.2.1 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	k8s.io/apimachinery v0.32.3 // indirect
	sigs.k8s.io/yaml v1.4.0 // indirect
	trpc.group/trpc-go/trpc-a2a-go v0.2.5 // indirect
	trpc.group/trpc-go/trpc-agent-go/storage/milvus v1.9.1-0.20260529112842-4f26702973ef // indirect
	trpc.group/trpc-go/trpc-agent-go/storage/postgres v1.9.1-0.20260529112842-4f26702973ef // indirect
	trpc.group/trpc-go/trpc-agent-go/storage/tcvector v1.9.1-0.20260529112842-4f26702973ef // indirect
)
