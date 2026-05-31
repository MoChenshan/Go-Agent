// Package pcg123 is the remote code executor for pcg 123
package pcg123

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	adminpb "git.woa.com/trpcprotocol/agent/code_executor_admin_admin"
	executorpb "git.woa.com/trpcprotocol/agent/code_executor_executor"

	proxypb "git.woa.com/trpcprotocol/agent/code_executor_proxy_proxy"
	"github.com/go-playground/validator/v10"
	"github.com/pkg/errors"
	"trpc.group/trpc-go/trpc-agent-go/codeexecutor"
	"trpc.group/trpc-go/trpc-agent-go/log"

	"git.woa.com/trpc-go/trpc-agent-go/trpc/codeexecutor/pcg123/internal/apigw"
	"git.woa.com/trpc-go/trpc-agent-go/trpc/codeexecutor/pcg123/internal/nfs/client"

	// Import the trpc-agent-go integration for code executor
	_ "git.woa.com/trpc-go/trpc-agent-go/trpc"
)

var (
	_ codeexecutor.CodeExecutor   = (*CodeExecutor)(nil)
	_ codeexecutor.EngineProvider = (*CodeExecutor)(nil)
)

// ReconnectMode defines the reconnection strategy
type ReconnectMode string

const (
	// ReconnectLazy reconnect on demand (default)
	ReconnectLazy ReconnectMode = "lazy"
	// ReconnectEager reconnect immediately when unhealthy
	ReconnectEager ReconnectMode = "eager"
)

// CodeExecutor is the remote code executor held by pcg-123, implement the codeexecutor.CodeExecutor interface
// Before using the code executor, you need to create a config and initialize the executor.
// SecretKey and SecretID are the credentials of the code executor, and can be obtained from the pcg-123 platform.
//
// By default the executor is constructed lazily: NewCodeExecutor only
// validates the configuration and the actual 123 sandbox (InitExecutor RPC)
// is allocated on the first ExecuteCode call or the first NFSRuntime
// method call. Pass WithLazyInit(false) to allocate the sandbox eagerly
// at construction time, which makes startup fail fast on misconfiguration
// at the cost of holding a sandbox per process.
//
// Example usage:
//
//	conf := pcg123.Config{
//		Language:  pcg123.LanguagePython310,
//		SecretKey: "your-secret-key",
//		SecretID:  "your-secret-id",
//	}
//
//	executor, cancel, err := pcg123.NewCodeExecutor(conf,
//		pcg123.WithExecuteTimeout(10*time.Second),
//		pcg123.WithIdleTimeout(15*time.Minute),
//		pcg123.WithInteractive(true),
//		pcg123.WithShared(true),
//		pcg123.WithProbeInterval(5*time.Second),
//		pcg123.WithReconnectMode(pcg123.ReconnectLazy),
//		pcg123.WithMaxFailedProbes(5),
//	)
//	if err != nil {
//		log.Fatal(err)
//	}
//
//	defer cancel()
type CodeExecutor struct {
	mu               sync.Mutex                      // hold the mutex for the code executor
	cfg              Config                          // the config of the code executor
	language         Language                        // the language of the code executor
	executeTimeout   time.Duration                   // the execute timeout of the code executor
	idleTimeout      time.Duration                   // the idle timeout of the code executor
	interactive      bool                            // if true, the code executor will be interactive
	shared           bool                            // if true, will use the shared executor
	clientID         string                          // the client id of the code executor
	executeURL       string                          // the executor url of the code executor
	nfsEndpoint      string                          // the NFS endpoint of the code executor
	workspaceMount   WorkspaceVolumeMount            // the workspace volume mount of the code executor
	delimiter        codeexecutor.CodeBlockDelimiter // the code block delimiter of the code executor
	reconnectMode    ReconnectMode                   // the reconnect mode of the code executor
	probeInterval    time.Duration                   // the probe interval of the code executor
	probeTimeout     time.Duration                   // the probe timeout of the code executor
	maxFailedProbes  int32                           // max consecutive failed probes before marking unhealthy
	healthy          atomic.Bool                     // the healthy status of the code executor
	failedProbeCount atomic.Int32                    // current consecutive failed probe count
	stopProbe        chan struct{}                   // the channel to stop the probe loop
	lazyInit         bool                            // defer InitExecutor until first use; default true
	inited           atomic.Bool                     // whether InitExecutor has ever succeeded; orthogonal to healthy
	initMu           sync.Mutex                      // serializes first-time Init; allows retry on failure (vs sync.Once)

	adminClient adminpb.AdminClientProxy
	proxyClient proxypb.ProxyClientProxy
	proxyHelper *apigw.ProxyHelper
	nfsClient   *client.Client
	engineOnce  sync.Once
	engine      codeexecutor.Engine

	// sessionIsolation controls whether NFSRuntime applies per-session
	// group-based POSIX isolation on each workspace (chgrp + setgid bit
	// + mode 2770, paired with setuid+setgid in the bash wrapper).
	// Default true; turn off via WithSessionIsolation(false) to fall
	// back to single-user behavior.
	sessionIsolation bool
}

// Option is the option of the code executor
type Option func(*CodeExecutor)

// WithExecuteTimeout set the execute timeout of the code executor
// If the execute timeout is not set, will use default execute timeout
func WithExecuteTimeout(executeTimeout time.Duration) Option {
	return func(e *CodeExecutor) {
		if executeTimeout <= 0 {
			executeTimeout = defaultExecuteTimeout
		}
		e.executeTimeout = executeTimeout
	}
}

// WithIdleTimeout set the idle timeout of the code executor
// If the idle timeout is not set, will use default idle timeout
func WithIdleTimeout(idleTimeout time.Duration) Option {
	return func(e *CodeExecutor) {
		if idleTimeout <= 0 {
			idleTimeout = defaultIdleTimeout
		}
		e.idleTimeout = idleTimeout
	}
}

// WithInteractive set the interactive mode of the code executor
func WithInteractive(interactive bool) Option {
	return func(e *CodeExecutor) {
		e.interactive = interactive
	}
}

// WithShared set the shared mode of the code executor.
// If shared is true, will use the shared executor, which is recommended for testing.
// Otherwise, will use executor isolated from others, which is recommended for production.
func WithShared(shared bool) Option {
	return func(e *CodeExecutor) {
		e.shared = shared
	}
}

// WithCodeBlockDelimiter set the code block delimiter for the code executor
// If the code block delimiter is not set, will use default code block delimiter
func WithCodeBlockDelimiter(delimiter codeexecutor.CodeBlockDelimiter) Option {
	return func(e *CodeExecutor) {
		e.delimiter = delimiter
	}
}

// WithProbeInterval set the probe interval for health check
// If the probe interval is not set, will use default probe interval
func WithProbeInterval(interval time.Duration) Option {
	return func(e *CodeExecutor) {
		if interval <= 0 {
			interval = defaultProbeInterval
		}
		e.probeInterval = interval
	}
}

// WithProbeTimeout set the probe timeout for health check
// If the probe timeout is not set, will use default probe timeout
func WithProbeTimeout(timeout time.Duration) Option {
	return func(e *CodeExecutor) {
		if timeout <= 0 {
			timeout = defaultProbeTimeout
		}
		e.probeTimeout = timeout
	}
}

// WithReconnectMode set the reconnect mode for the code executor
// ReconnectLazy: reconnect on demand when executing code (default)
// ReconnectEager: reconnect immediately when health check fails
func WithReconnectMode(mode ReconnectMode) Option {
	return func(e *CodeExecutor) {
		e.reconnectMode = mode
	}
}

// WithLazyInit controls whether the InitExecutor RPC (sandbox allocation)
// is deferred until the first ExecuteCode call or the first NFSRuntime
// method call. Lazy init is enabled by default so that processes which
// instantiate a CodeExecutor in main but rarely route requests through it
// (e.g. multi-replica services with sparse code execution traffic) no
// longer hold an idle 123 sandbox per node.
//
// Pass WithLazyInit(false) to restore the legacy behavior of allocating
// the sandbox synchronously during NewCodeExecutor, which is useful for
// short-lived programs or environments that want misconfiguration
// (credentials / network) to surface at startup.
//
// This option is orthogonal to ReconnectMode: ReconnectMode only governs
// repair after the executor has been inited at least once.
func WithLazyInit(enable bool) Option {
	return func(e *CodeExecutor) {
		e.lazyInit = enable
	}
}

// WithMaxFailedProbes set the max consecutive failed probes before marking unhealthy
// If the max failed probes is not set or set to 0, will use default value (3)
// When consecutive failed probes reaches this threshold, the executor will be marked as unhealthy
// and will not be automatically marked as healthy again by subsequent successful probes
func WithMaxFailedProbes(max int32) Option {
	return func(e *CodeExecutor) {
		if max <= 0 {
			max = defaultMaxFailedProbes
		}
		e.maxFailedProbes = max
	}
}

// WithSkillWorkspaceMount set the agent skill workspace volume mount for the code executor
// so that muti code executor can share the same workspace volume for distributed skill execution
// shared executor is not supported for skill workspace mount, so it will be set to false
func WithSkillWorkspaceMount(mount WorkspaceVolumeMount) Option {
	return func(e *CodeExecutor) {
		e.shared = false
		e.workspaceMount = mount
	}
}

// WithSessionIsolation toggles per-session POSIX isolation on the shared NFS
// workspace volume. Default is enabled.
//
// When enabled, NFSRuntime derives a deterministic per-session uid/gid from
// the workspace ID, chgrp's the workspace tree to that gid with mode 2770
// + setgid bit, and the bash wrapper drops privileges to (uid, gid) (via
// Python setuid+setgid+exec under passwordless sudo) before exec'ing the
// user command. The workspace's owner stays as the sandbox default user
// throughout, so the SDK side keeps full read/write access for staging,
// metadata, and output collection. Cross-session processes land in the
// "other" bucket of mode 2770 and are blocked at the directory.
//
// Disable only if you need to fall back to single-user behavior (e.g., a
// Skill that explicitly depends on running as the sandbox default user).
func WithSessionIsolation(enable bool) Option {
	return func(e *CodeExecutor) {
		e.sessionIsolation = enable
	}
}

var (
	defaultExecuteTimeout  = 5 * time.Second
	defaultIdleTimeout     = 15 * time.Minute
	defaultProbeInterval   = 1 * time.Second
	defaultProbeTimeout    = 2 * time.Second
	defaultMaxFailedProbes = int32(3)
	defaultDelimiter       = codeexecutor.CodeBlockDelimiter{Start: "```", End: "```"}
)

// NewCodeExecutor create a new code executor, cancel func must be called to destroy the executor
func NewCodeExecutor(cfg Config, opts ...Option) (*CodeExecutor, context.CancelFunc, error) {
	if err := validator.New().Struct(&cfg); err != nil {
		return nil, nil, errors.WithMessage(err, "validate config")
	}

	if !isValidLanguage(cfg.Language) {
		return nil, nil, errors.Errorf("invalid language: %s", cfg.Language)
	}

	ctx := context.Background()

	e := &CodeExecutor{
		language:         cfg.Language,
		executeTimeout:   defaultExecuteTimeout,
		idleTimeout:      defaultIdleTimeout,
		interactive:      false,
		shared:           false,
		delimiter:        defaultDelimiter,
		cfg:              cfg,
		reconnectMode:    ReconnectLazy,
		probeInterval:    defaultProbeInterval,
		probeTimeout:     defaultProbeTimeout,
		maxFailedProbes:  defaultMaxFailedProbes,
		stopProbe:        make(chan struct{}),
		lazyInit:         true,
		sessionIsolation: true,
	}

	for _, opt := range opts {
		opt(e)
	}

	cancel := func() {
		close(e.stopProbe)
		if err := e.destroyExecutor(ctx); err != nil {
			log.ErrorfContext(ctx, "destroy executor failed, err: %v", err)
		}
	}

	// In lazy mode (default) the sandbox is allocated on first use via
	// ensureReady. Only do the eager InitExecutor when the caller has
	// explicitly opted out via WithLazyInit(false). Routing through
	// ensureInited keeps the state-machine owner in one place.
	if !e.lazyInit {
		if err := e.ensureInited(ctx); err != nil {
			cancel()
			return nil, nil, err
		}
	}

	return e, cancel, nil
}

func (e *CodeExecutor) initExecutor(ctx context.Context, cfg Config) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	proxyHelper, err := apigw.NewProxyHelper(apigw.ProxyConfig{
		SecretKey: cfg.SecretKey,
		SecretID:  cfg.SecretID,
		Stage:     apigw.ReleaseStage,
	})
	if err != nil {
		return errors.WithMessage(err, "new proxy helper")
	}

	e.proxyHelper = proxyHelper
	e.adminClient = adminpb.NewAdminClientProxy()
	e.proxyClient = proxypb.NewProxyClientProxy()

	clientOpts, rspHeader := e.proxyHelper.NewTRPCClientPostOpts(rpcNameInitExecutor)
	initReq := adminpb.InitExecutorReq{
		Language:           string(e.language),
		Interactive:        e.interactive,
		IdleTimeoutSeconds: int32(e.idleTimeout.Seconds()),
		Shared:             e.shared,
	}
	e.fillInitReq(&initReq)
	rsp, err := e.adminClient.InitExecutor(ctx, &initReq, clientOpts...)
	if err != nil {
		return apigw.NewGatewayErr(err, rspHeader, rpcNameInitExecutor)
	}

	if rsp.Code != 0 {
		return apigw.NewBackendErr(rsp.Code, rspHeader, rsp.Msg, rpcNameInitExecutor)
	}

	e.clientID = rsp.ClientId
	e.executeURL = rsp.ExecuteCode_URL
	e.nfsEndpoint = rsp.NfsEndpoint

	nfsClient, err := client.NewNFSClient(e.nfsEndpoint)
	if err != nil {
		return errors.WithMessage(err, "create nfs client")
	}

	e.nfsClient = nfsClient
	e.healthy.Store(true)

	go e.startProbeLoop(ctx)
	return nil
}

func (e *CodeExecutor) fillInitReq(req *adminpb.InitExecutorReq) {
	if e.workspaceMount.CFSVolumeMount != nil {
		req.WorkspaceMount = &adminpb.InitExecutorReq_Cfs{
			Cfs: &executorpb.CFSVolumeMount{
				Name:    e.workspaceMount.CFSVolumeMount.Name,
				Host:    e.workspaceMount.CFSVolumeMount.Host,
				Path:    e.workspaceMount.CFSVolumeMount.Path,
				Version: string(e.workspaceMount.CFSVolumeMount.Version),
			},
		}
	}
}

func (e *CodeExecutor) destroyExecutor(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.nfsClient != nil {
		if err := e.nfsClient.Close(); err != nil {
			log.ErrorfContext(ctx, "nfs close failed, err: %v", err)
		}
	}

	if len(e.clientID) == 0 {
		return nil
	}

	clientOpts, rspHeader := e.proxyHelper.NewTRPCClientPostOpts(rpcNameDestroyExecutor)
	rsp, err := e.adminClient.DestroyExecutor(ctx, &adminpb.DestroyExecutorReq{
		ClientId: e.clientID,
	}, clientOpts...)
	if err != nil {
		return apigw.NewGatewayErr(err, rspHeader, rpcNameDestroyExecutor)
	}
	if rsp.Code != 0 {
		return apigw.NewBackendErr(rsp.Code, rspHeader, rsp.Msg, rpcNameDestroyExecutor)
	}
	return nil
}

func (e *CodeExecutor) startProbeLoop(ctx context.Context) {
	ticker := time.NewTicker(e.probeInterval)
	defer ticker.Stop()

	for {
		select {
		case <-e.stopProbe:
			return
		case <-ticker.C:
			if e.checkHealth(ctx) {
				e.failedProbeCount.Store(0)
				continue
			}

			if e.failedProbeCount.Add(1) < e.maxFailedProbes {
				continue
			}

			e.healthy.Store(false)
			log.InfofContext(ctx, "executor marked as unhealthy after %d failed probes", e.failedProbeCount.Load())
			if e.reconnectMode != ReconnectEager {
				return
			}

			log.InfofContext(ctx, "health check failed for eager model, reconnecting...")
			if err := e.reconnect(ctx); err != nil {
				log.ErrorfContext(ctx, "reconnect failed: %v, will retry...", err)
			} else {
				return
			}
		}
	}
}

func (e *CodeExecutor) checkHealth(ctx context.Context) bool {
	e.mu.Lock()
	nfsClient := e.nfsClient
	e.mu.Unlock()

	timer := time.NewTimer(e.probeTimeout)
	defer timer.Stop()

	result := make(chan error, 1)
	go func() {
		result <- nfsClient.HealthCheck()
	}()

	select {
	case err := <-result:
		if err != nil {
			log.DebugfContext(ctx, "nfs health check failed, err: %v", err)
			return false
		}
		return true
	case <-timer.C:
		log.DebugfContext(ctx, "nfs health check timeout after %s", e.probeTimeout)
		return false
	case <-ctx.Done():
		log.DebugfContext(ctx, "nfs health check canceled, err: %v", ctx.Err())
		return false
	}
}

func (e *CodeExecutor) reconnect(ctx context.Context) error {
	_ = e.destroyExecutor(ctx)
	return e.initExecutor(ctx, e.cfg)
}

// ExecuteCode execute the code blocks provided in the input and returns the result
// This method will execute the code blocks one by one, and return the combined result.
// If the code block is empty, the method will skip the code block.
// If any code block fails, the method will continue to execute the next code block, and return the combined result.
// The language of the code executor is set by the config, and cannot be changed after the executor is created.
//
// Example usage:
//
//	result, err := executor.ExecuteCode(context.Background(), codeexecutor.CodeExecutionInput{
//		CodeBlocks: []codeexecutor.CodeBlock{
//			{Code: "print('Hello, World!')"},
//		},
//	})
//	if err != nil {
//		log.Fatal(err)
//	}
//
//	fmt.Println(result.Output)
//	for _, file := range result.OutputFiles {
//		fmt.Println(file.Name, file.Content, file.MIMEType)
//	}
func (e *CodeExecutor) ExecuteCode(ctx context.Context,
	input codeexecutor.CodeExecutionInput) (codeexecutor.CodeExecutionResult, error) {
	if err := e.ensureReady(ctx); err != nil {
		return codeexecutor.CodeExecutionResult{}, err
	}

	return e.executeCode(ctx, input)
}

func (e *CodeExecutor) ensureReady(ctx context.Context) error {
	if err := e.ensureInited(ctx); err != nil {
		return err
	}
	return e.ensureHealth(ctx)
}

func (e *CodeExecutor) ensureInited(ctx context.Context) error {
	if e.inited.Load() {
		return nil
	}

	e.initMu.Lock()
	defer e.initMu.Unlock()
	if e.inited.Load() {
		return nil
	}

	if err := e.initExecutor(ctx, e.cfg); err != nil {
		// Best-effort cleanup: if InitExecutor succeeded on the server
		// but a later local step (e.g. NFS client creation) failed,
		// the sandbox would otherwise leak until its idle timeout.
		if destroyErr := e.destroyExecutor(ctx); destroyErr != nil {
			log.WarnfContext(ctx, "destroy executor after failed init: %v", destroyErr)
		}
		return err
	}
	e.inited.Store(true)
	return nil
}

func (e *CodeExecutor) ensureHealth(ctx context.Context) error {
	if e.healthy.Load() {
		return nil
	}

	if e.reconnectMode != ReconnectLazy {
		return errors.New("executor is unhealthy, perhaps eager reconnect is not working")
	}

	log.InfofContext(ctx, "executor unhealthy, lazy reconnecting...")
	if err := e.reconnect(ctx); err != nil {
		return errors.WithMessage(err, "reconnect failed")
	}
	return nil
}

func (e *CodeExecutor) executeCode(ctx context.Context,
	input codeexecutor.CodeExecutionInput) (codeexecutor.CodeExecutionResult, error) {
	var (
		output      strings.Builder
		outputFiles []codeexecutor.File
	)

	for i, block := range input.CodeBlocks {
		if block.Code == "" {
			continue
		}
		result, err := e.executeCodeBlock(ctx, block)
		if err != nil {
			output.WriteString(fmt.Sprintf("Error executing code block %d: %v\n", i, err))
			continue
		}
		output.WriteString(result.Output)
		for j, file := range result.OutputFiles {
			result.OutputFiles[j].Name = fmt.Sprintf("code_block_%d_%s", i, file.Name)
		}
		outputFiles = append(outputFiles, result.OutputFiles...)
	}

	return codeexecutor.CodeExecutionResult{
		Output:      output.String(),
		OutputFiles: outputFiles,
	}, nil
}

func (e *CodeExecutor) executeCodeBlock(ctx context.Context,
	block codeexecutor.CodeBlock) (codeexecutor.CodeExecutionResult, error) {
	result, files, err := e.runCodeBlock(ctx, block)
	if err != nil {
		return codeexecutor.CodeExecutionResult{}, err
	}

	return codeexecutor.CodeExecutionResult{
		Output:      compileOutput(result),
		OutputFiles: getOutputFiles(ctx, files),
	}, nil
}

func (e *CodeExecutor) runCodeBlock(ctx context.Context,
	block codeexecutor.CodeBlock) (codeexecutor.RunResult, []string, error) {
	var empty codeexecutor.RunResult
	clientOpts, rspHeader, err := e.proxyHelper.NewHTTPClientPostOpts(e.executeURL)
	if err != nil {
		return empty, nil, errors.WithMessage(err, "new http client post opts")
	}

	begin := time.Now()
	rsp, err := e.proxyClient.ExecuteCode(ctx, &proxypb.ExecuteCodeReq{
		ClientId:       e.clientID,
		Code:           block.Code,
		TimeoutSeconds: int32(e.executeTimeout.Seconds()),
	}, clientOpts...)
	if err != nil {
		return empty, nil, apigw.NewGatewayErr(err, rspHeader, rpcNameExecuteCode)
	}

	if rsp.Code != 0 {
		return empty, nil, apigw.NewBackendErr(rsp.Code, rspHeader, rsp.Msg, rpcNameExecuteCode)
	}

	return codeexecutor.RunResult{
		Stdout:   rsp.Stdout,
		Stderr:   rsp.Stderr,
		ExitCode: int(rsp.ExitCode),
		Duration: time.Since(begin),
	}, rsp.Images, nil
}

func compileOutput(result codeexecutor.RunResult) string {
	var output strings.Builder
	if result.ExitCode != 0 {
		output.WriteString(fmt.Sprintf("Process exited with code: %d, ", result.ExitCode))
	}
	if result.Stdout != "" {
		output.WriteString(result.Stdout)
	}
	if result.Stderr != "" {
		output.WriteString(result.Stderr)
	}
	return output.String()
}

// dataURL format: "data:[<mediatype>][;base64],<base64data>"
// <mediatype> is the mime type of the image, e.g. "image/png", "image/jpeg", etc.
// <base64data> is the base64 encoded data of the image
var dataURLRegexp = regexp.MustCompile(`^data:([^;]+);base64,(.+)$`)

func getOutputFiles(ctx context.Context, images []string) []codeexecutor.File {
	files := make([]codeexecutor.File, 0)
	for i, dataURL := range images {
		matches := dataURLRegexp.FindStringSubmatch(dataURL)
		if len(matches) != 3 {
			log.ErrorfContext(ctx, "invalid dataURL format, dataURL: %s", dataURL)
			continue
		}
		mediaType := matches[1]
		base64Data := matches[2]
		files = append(files, codeexecutor.File{
			Name:     fmt.Sprintf("image.%d.%s", i, strings.TrimPrefix(mediaType, "image/")),
			Content:  base64Data,
			MIMEType: mediaType,
		})
	}
	return files
}

// CodeBlockDelimiter return the delimiter of the code block
func (e *CodeExecutor) CodeBlockDelimiter() codeexecutor.CodeBlockDelimiter {
	return e.delimiter
}

// Engine returns the hybrid engine for Skills support.
// The engine combines NFS-based workspace management with remote code execution.
func (e *CodeExecutor) Engine() codeexecutor.Engine {
	if !enableSkills(e.language) {
		log.Warnf("remote agent skill engine is disabled for language %s", e.language)
		return nil
	}

	e.engineOnce.Do(func() {
		rt := NewNFSRuntime(e)
		e.engine = codeexecutor.NewEngine(rt, rt, rt)
	})

	return e.engine
}

// NFSClient return nfs client
func (e *CodeExecutor) NFSClient() *client.Client {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.nfsClient
}
