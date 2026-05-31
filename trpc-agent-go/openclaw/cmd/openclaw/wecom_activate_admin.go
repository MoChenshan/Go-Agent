package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	wecomchannel "git.woa.com/trpc-go/trpc-agent-go/openclaw/channel/wecom"
	occhannel "trpc.group/trpc-go/trpc-agent-go/openclaw/channel"
)

const (
	wecomActivateRuntimesPath = "/api/channels/wecom/runtimes"
	wecomActivateActionPath   = "/api/channels/wecom/activate"

	wecomActivateFormRuntimeKey = "runtime_key"
	wecomActivateFormUserID     = "wecom_user_id"
	wecomActivateFormScene      = "scene"
	wecomActivateFormRequestID  = "client_request_id"
	wecomActivateFormReturnPath = "return_path"

	wecomActivateSceneAdmin = "admin_manual"
	wecomActivateSceneAPI   = "api"

	wecomActivateAnchorPrefix = "wecom-runtime-"
	wecomActivateKeyPrefix    = "wecom_rt_"
	wecomActivateKeyBytes     = 8

	wecomActivateReasonAIModeRequired = "ai_mode_required"
	wecomActivateReasonWebSocketMode  = "websocket_mode_required"
	wecomActivateReasonNotConnected   = "not_connected"

	wecomActivateCooldown = 30 * time.Second
	wecomActivateKind     = "activation"

	wecomActivateQueryNotice = "notice"
	wecomActivateQueryError  = "error"

	wecomActivateNoticeSent = "WeCom activation sent."
	wecomActivateNoticeDup  = "WeCom activation was already sent " +
		"recently."

	wecomActivateErrInvalid      = "invalid_request"
	wecomActivateErrRuntimeGone  = "runtime_not_found"
	wecomActivateErrUnsupported  = "runtime_not_supported"
	wecomActivateErrDisconnected = "runtime_not_connected"
	wecomActivateErrBlocked      = "target_not_allowed"
	wecomActivateErrCooldown     = "cooldown_active"
	wecomActivateErrDelivery     = "delivery_failed"

	wecomActivateDefaultUserEnvName = "TRPC_CLAW_USER_NAME"

	wecomActivateMsg = "你好。\n\n你已经成功定位到当前企微 Bot。\n" +
		"后续直接在这个会话里给我发消息即可。"
)

type wecomAdminActivationStatusProvider interface {
	WeComAdminActivationStatus() wecomchannel.AdminActivationStatus
}

type wecomAdminActivationUserProvider interface {
	WeComAdminAllowsUser(userID string) bool
}

type wecomActivateAdminService struct {
	mu sync.Mutex

	runtimes   []*wecomActivateRuntime
	index      map[string]*wecomActivateRuntime
	byIdentity map[string]*wecomActivateRuntime

	recentRequests map[string]wecomActivateResponse
	recentTargets  map[string]time.Time
}

type wecomActivateRuntime struct {
	identity      string
	key           string
	title         string
	target        wecomchannel.AdminTarget
	defaultUserID string

	sender         occhannel.TextSender
	statusProvider wecomAdminActivationStatusProvider
	userProvider   wecomAdminActivationUserProvider
}

type wecomActivateRuntimeList struct {
	Runtimes []wecomActivateRuntimeView `json:"runtimes"`
}

type wecomActivateViews = []wecomActivateRuntimeView

type wecomActivateRuntimeView struct {
	RuntimeKey         string                  `json:"runtime_key"`
	Title              string                  `json:"title"`
	Name               string                  `json:"name,omitempty"`
	BotMode            string                  `json:"bot_mode,omitempty"`
	ConnectionMode     string                  `json:"connection_mode,omitempty"`
	ChatPolicy         string                  `json:"chat_policy,omitempty"`
	DefaultWeComUserID string                  `json:"default_wecom_user_id,omitempty"`
	Activation         wecomActivateStatusView `json:"activation"`
}

type wecomActivateStatusView struct {
	Supported bool   `json:"supported"`
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}

type wecomActivateRequest struct {
	RuntimeKey      string `json:"runtime_key"`
	WeComUserID     string `json:"wecom_user_id"`
	Scene           string `json:"scene,omitempty"`
	ClientRequestID string `json:"client_request_id,omitempty"`
	ReturnPath      string `json:"-"`
}

type wecomActivateResponse struct {
	OK           bool      `json:"ok"`
	RuntimeKey   string    `json:"runtime_key"`
	WeComUserID  string    `json:"wecom_user_id"`
	Target       string    `json:"target"`
	MessageKind  string    `json:"message_kind"`
	SentAt       time.Time `json:"sent_at"`
	Deduplicated bool      `json:"deduplicated,omitempty"`
}

type wecomActivateError struct {
	Status  int
	Code    string
	Message string
}

func (e *wecomActivateError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

type wecomActivateErrorResponse struct {
	OK    bool                   `json:"ok"`
	Error wecomActivateErrorBody `json:"error"`
}

type wecomActivateErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func newWeComActivateAdminService(
	channels []occhannel.Channel,
) *wecomActivateAdminService {
	runtimes := collectWeComActivateRuntimes(channels)
	return &wecomActivateAdminService{
		runtimes:       runtimes,
		index:          map[string]*wecomActivateRuntime{},
		byIdentity:     map[string]*wecomActivateRuntime{},
		recentRequests: map[string]wecomActivateResponse{},
		recentTargets:  map[string]time.Time{},
	}
}

func collectWeComActivateRuntimes(
	channels []occhannel.Channel,
) []*wecomActivateRuntime {
	runtimes := make([]*wecomActivateRuntime, 0, len(channels))
	seen := make(map[string]struct{}, len(channels))
	for _, ch := range channels {
		targetProvider, ok := ch.(wecomAdminTargetProvider)
		if !ok || targetProvider == nil {
			continue
		}
		statusProvider, ok := ch.(wecomAdminActivationStatusProvider)
		if !ok || statusProvider == nil {
			continue
		}
		userProvider, ok := ch.(wecomAdminActivationUserProvider)
		if !ok || userProvider == nil {
			continue
		}
		sender, ok := ch.(occhannel.TextSender)
		if !ok || sender == nil {
			continue
		}

		target := targetProvider.WeComAdminTarget()
		identity := buildWeComRuntimeIdentity(target)
		if identity == "" {
			continue
		}
		if _, exists := seen[identity]; exists {
			continue
		}
		seen[identity] = struct{}{}
		runtimes = append(runtimes, &wecomActivateRuntime{
			identity: identity,
			key:      buildWeComActivateRuntimeKey(target),
			title: channelRuntimeTitle(
				"WeCom Runtime",
				target.Name,
			),
			target:         target,
			defaultUserID:  readWeComActivateDefaultUserID(target.StateDir),
			sender:         sender,
			statusProvider: statusProvider,
			userProvider:   userProvider,
		})
	}
	sort.SliceStable(runtimes, func(i, j int) bool {
		left := strings.TrimSpace(runtimes[i].title)
		right := strings.TrimSpace(runtimes[j].title)
		if left == right {
			return runtimes[i].key < runtimes[j].key
		}
		return left < right
	})
	return runtimes
}

func buildWeComRuntimeIdentity(
	target wecomchannel.AdminTarget,
) string {
	parts := []string{
		strings.TrimSpace(target.Name),
		strings.TrimSpace(target.BotMode),
		strings.TrimSpace(target.ConnectionMode),
		strings.TrimSpace(target.CallbackPath),
		strings.TrimSpace(target.StateDir),
	}
	if isAllEmpty(parts...) {
		return ""
	}
	return strings.Join(parts, "|")
}

func buildWeComActivateRuntimeKey(
	target wecomchannel.AdminTarget,
) string {
	identity := strings.Join(
		[]string{
			buildWeComRuntimeIdentity(target),
			strings.TrimSpace(target.AIBotID),
		},
		"|",
	)
	if identity == "|" {
		return ""
	}
	sum := sha256.Sum256([]byte(identity))
	return wecomActivateKeyPrefix + hex.EncodeToString(
		sum[:wecomActivateKeyBytes],
	)
}

func isAllEmpty(values ...string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return false
		}
	}
	return true
}

func (s *wecomActivateAdminService) ensureIndex() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.index) != 0 || len(s.byIdentity) != 0 {
		return
	}
	for _, runtime := range s.runtimes {
		if runtime == nil {
			continue
		}
		s.index[runtime.key] = runtime
		s.byIdentity[runtime.identity] = runtime
	}
}

func (s *wecomActivateAdminService) runtimeViewList() wecomActivateViews {
	if s == nil {
		return nil
	}
	s.ensureIndex()

	views := make(
		[]wecomActivateRuntimeView,
		0,
		len(s.runtimes),
	)
	for _, runtime := range s.runtimes {
		if runtime == nil {
			continue
		}
		views = append(views, runtime.view())
	}
	return views
}

func (s *wecomActivateAdminService) runtimeViewByIdentity(
	identity string,
) (wecomActivateRuntimeView, bool) {
	if s == nil {
		return wecomActivateRuntimeView{}, false
	}
	s.ensureIndex()
	runtime, ok := s.byIdentity[strings.TrimSpace(identity)]
	if !ok || runtime == nil {
		return wecomActivateRuntimeView{}, false
	}
	return runtime.view(), true
}

func (r *wecomActivateRuntime) view() wecomActivateRuntimeView {
	if r == nil {
		return wecomActivateRuntimeView{}
	}
	status := wecomchannel.AdminActivationStatus{}
	if r.statusProvider != nil {
		status = r.statusProvider.WeComAdminActivationStatus()
	}
	return wecomActivateRuntimeView{
		RuntimeKey:         strings.TrimSpace(r.key),
		Title:              strings.TrimSpace(r.title),
		Name:               strings.TrimSpace(r.target.Name),
		BotMode:            strings.TrimSpace(r.target.BotMode),
		ConnectionMode:     strings.TrimSpace(r.target.ConnectionMode),
		ChatPolicy:         strings.TrimSpace(r.target.ChatPolicy),
		DefaultWeComUserID: strings.TrimSpace(r.defaultUserID),
		Activation:         activationStatusView(status),
	}
}

func readWeComActivateDefaultUserID(stateDir string) string {
	values, err := readRuntimeEnvAssignmentsForStateDir(stateDir)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(
		values[wecomActivateDefaultUserEnvName],
	)
}

func activationStatusView(
	status wecomchannel.AdminActivationStatus,
) wecomActivateStatusView {
	return wecomActivateStatusView{
		Supported: status.Supported,
		Available: status.Available,
		Reason:    strings.TrimSpace(status.Reason),
	}
}

func wrapWeComActivateAdminHandler(
	base http.Handler,
	service *wecomActivateAdminService,
) http.Handler {
	if service == nil {
		return base
	}

	mux := http.NewServeMux()
	mux.HandleFunc(
		wecomActivateRuntimesPath,
		service.handleRuntimes,
	)
	mux.HandleFunc(
		wecomActivateActionPath,
		service.handleActivate,
	)
	if base != nil {
		mux.Handle("/", base)
	}
	return mux
}

func (s *wecomActivateAdminService) handleRuntimes(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.Method != http.MethodGet {
		http.Error(
			w,
			"method not allowed",
			http.StatusMethodNotAllowed,
		)
		return
	}
	writeRuntimeJSON(
		w,
		http.StatusOK,
		wecomActivateRuntimeList{
			Runtimes: s.runtimeViewList(),
		},
	)
}

func (s *wecomActivateAdminService) handleActivate(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.Method != http.MethodPost {
		http.Error(
			w,
			"method not allowed",
			http.StatusMethodNotAllowed,
		)
		return
	}

	request, jsonMode, err := parseWeComActivateRequest(r)
	if err != nil {
		s.writeActivateFailure(
			w,
			r,
			jsonMode,
			newWeComActivateError(
				http.StatusBadRequest,
				wecomActivateErrInvalid,
				"Invalid activate request.",
			),
		)
		return
	}

	response, activateErr := s.activate(
		r.Context(),
		request,
	)
	if activateErr != nil {
		s.writeActivateFailure(
			w,
			r,
			jsonMode,
			activateErr,
		)
		return
	}
	s.writeActivateSuccess(
		w,
		r,
		jsonMode,
		request,
		response,
	)
}

func parseWeComActivateRequest(
	r *http.Request,
) (wecomActivateRequest, bool, error) {
	if isWeComActivateJSONRequest(r) {
		return parseWeComActivateJSONRequest(r)
	}
	request, err := parseWeComActivateFormRequest(r)
	return request, false, err
}

func isWeComActivateJSONRequest(r *http.Request) bool {
	if r == nil {
		return false
	}
	contentType := strings.ToLower(strings.TrimSpace(
		r.Header.Get("Content-Type"),
	))
	return strings.HasPrefix(
		contentType,
		"application/json",
	)
}

func parseWeComActivateJSONRequest(
	r *http.Request,
) (wecomActivateRequest, bool, error) {
	var request wecomActivateRequest
	if r == nil || r.Body == nil {
		return request, true, errors.New("missing request body")
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return request, true, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return request, true, errors.New("unexpected trailing data")
	}
	return normalizeWeComActivateRequest(request), true, nil
}

func parseWeComActivateFormRequest(
	r *http.Request,
) (wecomActivateRequest, error) {
	var request wecomActivateRequest
	if r == nil {
		return request, errors.New("nil request")
	}
	if err := r.ParseForm(); err != nil {
		return request, err
	}
	request = wecomActivateRequest{
		RuntimeKey: r.FormValue(
			wecomActivateFormRuntimeKey,
		),
		WeComUserID: r.FormValue(
			wecomActivateFormUserID,
		),
		Scene: r.FormValue(
			wecomActivateFormScene,
		),
		ClientRequestID: r.FormValue(
			wecomActivateFormRequestID,
		),
		ReturnPath: r.FormValue(
			wecomActivateFormReturnPath,
		),
	}
	return normalizeWeComActivateRequest(request), nil
}

func normalizeWeComActivateRequest(
	request wecomActivateRequest,
) wecomActivateRequest {
	request.RuntimeKey = strings.TrimSpace(request.RuntimeKey)
	request.WeComUserID = strings.TrimSpace(request.WeComUserID)
	request.Scene = strings.TrimSpace(request.Scene)
	request.ClientRequestID = strings.TrimSpace(
		request.ClientRequestID,
	)
	request.ReturnPath = sanitizeWeComActivateReturnPath(
		request.ReturnPath,
	)
	return request
}

func sanitizeWeComActivateReturnPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return channelsAdminPagePath
	}
	parsed, err := url.Parse(path)
	if err != nil {
		return channelsAdminPagePath
	}
	if strings.TrimSpace(parsed.Scheme) != "" ||
		strings.TrimSpace(parsed.Host) != "" {
		return channelsAdminPagePath
	}
	targetPath := strings.TrimSpace(parsed.Path)
	if targetPath == "" {
		targetPath = channelsAdminPagePath
	}
	parsed.Path = adminCleanURLPath(targetPath)
	return parsed.String()
}

func (s *wecomActivateAdminService) activate(
	ctx context.Context,
	request wecomActivateRequest,
) (wecomActivateResponse, *wecomActivateError) {
	request = normalizeWeComActivateRequest(request)
	if request.RuntimeKey == "" {
		return wecomActivateResponse{}, newWeComActivateError(
			http.StatusBadRequest,
			wecomActivateErrInvalid,
			"`runtime_key` is required.",
		)
	}

	s.ensureIndex()
	runtime := s.index[request.RuntimeKey]
	if runtime == nil {
		return wecomActivateResponse{}, newWeComActivateError(
			http.StatusNotFound,
			wecomActivateErrRuntimeGone,
			"The selected WeCom runtime was not found.",
		)
	}
	request.WeComUserID = resolveWeComActivateUserID(
		request.WeComUserID,
		runtime.defaultUserID,
	)
	if request.WeComUserID == "" {
		return wecomActivateResponse{}, newWeComActivateError(
			http.StatusBadRequest,
			wecomActivateErrInvalid,
			"`wecom_user_id` is required when no default "+
				"creator RTX is available.",
		)
	}

	status := runtime.statusProvider.WeComAdminActivationStatus()
	if !status.Supported {
		return wecomActivateResponse{}, newWeComActivateError(
			http.StatusConflict,
			wecomActivateErrUnsupported,
			"The selected WeCom runtime does not support "+
				"activation.",
		)
	}
	if !status.Available {
		return wecomActivateResponse{}, newWeComActivateError(
			http.StatusConflict,
			wecomActivateErrDisconnected,
			"The selected WeCom runtime is not currently "+
				"connected.",
		)
	}
	if !runtime.userProvider.WeComAdminAllowsUser(
		request.WeComUserID,
	) {
		return wecomActivateResponse{}, newWeComActivateError(
			http.StatusForbidden,
			wecomActivateErrBlocked,
			"The target user is not allowed by the current "+
				"WeCom chat policy.",
		)
	}

	now := time.Now()
	if response, ok := s.cachedRequestResponse(request, now); ok {
		response.Deduplicated = true
		return response, nil
	}
	if s.cooldownActive(request, now) {
		return wecomActivateResponse{}, newWeComActivateError(
			http.StatusTooManyRequests,
			wecomActivateErrCooldown,
			"The same activation target was triggered too "+
				"recently.",
		)
	}

	target := wecomchannel.BuildAdminDirectMessageTarget(
		request.WeComUserID,
	)
	if target == "" {
		return wecomActivateResponse{}, newWeComActivateError(
			http.StatusBadRequest,
			wecomActivateErrInvalid,
			"Invalid WeCom user target.",
		)
	}
	if err := runtime.sender.SendText(
		ctx,
		target,
		buildWeComActivateMessage(request.Scene),
	); err != nil {
		latest := runtime.statusProvider.WeComAdminActivationStatus()
		if latest.Supported && !latest.Available {
			return wecomActivateResponse{}, newWeComActivateError(
				http.StatusConflict,
				wecomActivateErrDisconnected,
				"The selected WeCom runtime is not currently "+
					"connected.",
			)
		}
		return wecomActivateResponse{}, newWeComActivateError(
			http.StatusBadGateway,
			wecomActivateErrDelivery,
			"Failed to deliver the activation message.",
		)
	}

	response := wecomActivateResponse{
		OK:          true,
		RuntimeKey:  request.RuntimeKey,
		WeComUserID: request.WeComUserID,
		Target:      target,
		MessageKind: wecomActivateKind,
		SentAt:      now,
	}
	s.rememberRequestSuccess(request, response, now)
	return response, nil
}

func resolveWeComActivateUserID(
	requestUserID string,
	defaultUserID string,
) string {
	requestUserID = strings.TrimSpace(requestUserID)
	if requestUserID != "" {
		return requestUserID
	}
	return strings.TrimSpace(defaultUserID)
}

func buildWeComActivateMessage(scene string) string {
	scene = strings.TrimSpace(scene)
	if scene == "" {
		return wecomActivateMsg
	}
	return wecomActivateMsg
}

func wecomActivateAnchor(runtimeKey string) string {
	runtimeKey = strings.TrimSpace(runtimeKey)
	if runtimeKey == "" {
		return ""
	}
	return wecomActivateAnchorPrefix +
		sanitizeWeComActivateAnchorValue(runtimeKey)
}

func sanitizeWeComActivateAnchorValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	var builder strings.Builder
	builder.Grow(len(value))
	lastDash := false
	for _, r := range value {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r):
			builder.WriteRune(unicode.ToLower(r))
			lastDash = false
		case r == '-' || r == '_':
			builder.WriteRune(r)
			lastDash = false
		default:
			if !lastDash {
				builder.WriteByte('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(builder.String(), "-")
}

func describeWeComActivateStatus(
	status wecomActivateStatusView,
) string {
	if status.Available {
		return "Ready to send a fixed activation message to a " +
			"WeCom user ID."
	}
	switch strings.TrimSpace(status.Reason) {
	case "":
		return ""
	case wecomActivateReasonAIModeRequired:
		return "Activation requires `bot_mode=ai`."
	case wecomActivateReasonWebSocketMode:
		return "Activation requires `connection_mode=websocket`."
	case wecomActivateReasonNotConnected:
		return "Activation requires a live WeCom websocket " +
			"connection."
	default:
		return "Activation is not available for the current " +
			"WeCom runtime."
	}
}

func (s *wecomActivateAdminService) cachedRequestResponse(
	request wecomActivateRequest,
	now time.Time,
) (wecomActivateResponse, bool) {
	if s == nil || request.ClientRequestID == "" {
		return wecomActivateResponse{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(now)
	response, ok := s.recentRequests[wecomActivateRequestCacheKey(request)]
	if !ok {
		return wecomActivateResponse{}, false
	}
	return response, true
}

func (s *wecomActivateAdminService) cooldownActive(
	request wecomActivateRequest,
	now time.Time,
) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(now)
	lastSent, ok := s.recentTargets[wecomActivateTargetCacheKey(request)]
	if !ok {
		return false
	}
	return now.Sub(lastSent) < wecomActivateCooldown
}

func (s *wecomActivateAdminService) rememberRequestSuccess(
	request wecomActivateRequest,
	response wecomActivateResponse,
	now time.Time,
) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(now)
	s.recentTargets[wecomActivateTargetCacheKey(request)] = now
	if request.ClientRequestID != "" {
		s.recentRequests[wecomActivateRequestCacheKey(request)] = response
	}
}

func (s *wecomActivateAdminService) pruneLocked(now time.Time) {
	cutoff := now.Add(-wecomActivateCooldown)
	for key, sentAt := range s.recentTargets {
		if sentAt.Before(cutoff) {
			delete(s.recentTargets, key)
		}
	}
	for key, response := range s.recentRequests {
		if response.SentAt.Before(cutoff) {
			delete(s.recentRequests, key)
		}
	}
}

func wecomActivateTargetCacheKey(
	request wecomActivateRequest,
) string {
	return strings.Join(
		[]string{
			strings.TrimSpace(request.RuntimeKey),
			strings.TrimSpace(request.WeComUserID),
		},
		"|",
	)
}

func wecomActivateRequestCacheKey(
	request wecomActivateRequest,
) string {
	return strings.Join(
		[]string{
			strings.TrimSpace(request.RuntimeKey),
			strings.TrimSpace(request.WeComUserID),
			strings.TrimSpace(request.ClientRequestID),
		},
		"|",
	)
}

func (s *wecomActivateAdminService) writeActivateFailure(
	w http.ResponseWriter,
	r *http.Request,
	jsonMode bool,
	err *wecomActivateError,
) {
	if err == nil {
		err = newWeComActivateError(
			http.StatusInternalServerError,
			wecomActivateErrDelivery,
			http.StatusText(http.StatusInternalServerError),
		)
	}
	if jsonMode {
		writeRuntimeJSON(
			w,
			err.Status,
			wecomActivateErrorResponse{
				OK: false,
				Error: wecomActivateErrorBody{
					Code:    err.Code,
					Message: err.Message,
				},
			},
		)
		return
	}
	writeAdminRedirect(
		w,
		wecomActivateRedirectLocation(
			r.URL.Path,
			sanitizeWeComActivateReturnPath(
				r.FormValue(wecomActivateFormReturnPath),
			),
			wecomActivateQueryError,
			err.Message,
		),
		http.StatusSeeOther,
	)
}

func (s *wecomActivateAdminService) writeActivateSuccess(
	w http.ResponseWriter,
	r *http.Request,
	jsonMode bool,
	request wecomActivateRequest,
	response wecomActivateResponse,
) {
	if jsonMode {
		writeRuntimeJSON(w, http.StatusOK, response)
		return
	}
	notice := wecomActivateNoticeSent
	if response.Deduplicated {
		notice = wecomActivateNoticeDup
	}
	writeAdminRedirect(
		w,
		wecomActivateRedirectLocation(
			r.URL.Path,
			request.ReturnPath,
			wecomActivateQueryNotice,
			notice,
		),
		http.StatusSeeOther,
	)
}

func wecomActivateRedirectLocation(
	currentPath string,
	returnPath string,
	queryKey string,
	message string,
) string {
	values := url.Values{}
	message = strings.TrimSpace(message)
	if message != "" {
		values.Set(queryKey, message)
	}
	returnPath = sanitizeWeComActivateReturnPath(returnPath)
	location := returnPath
	parsed, err := url.Parse(returnPath)
	if err == nil {
		parsedValues := parsed.Query()
		for key, entries := range values {
			for _, entry := range entries {
				parsedValues.Set(key, entry)
			}
		}
		parsed.RawQuery = parsedValues.Encode()
		location = parsed.String()
	}
	return adminRelativeReference(currentPath, location)
}

func newWeComActivateError(
	status int,
	code string,
	message string,
) *wecomActivateError {
	return &wecomActivateError{
		Status:  status,
		Code:    strings.TrimSpace(code),
		Message: strings.TrimSpace(message),
	}
}
