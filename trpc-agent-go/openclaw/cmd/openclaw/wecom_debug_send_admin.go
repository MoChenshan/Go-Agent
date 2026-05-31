package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	wecomchannel "git.woa.com/trpc-go/trpc-agent-go/openclaw/channel/wecom"
	occhannel "trpc.group/trpc-go/trpc-agent-go/openclaw/channel"
)

const (
	wecomDebugSendActionPath = "/api/channels/wecom/debug/send"

	wecomDebugSendFormRuntimeKey = "runtime_key"
	wecomDebugSendFormTarget     = "target"
	wecomDebugSendFormText       = "text"
	wecomDebugSendFormFilePath   = "file_path"
	wecomDebugSendFormFileName   = "file_name"
	wecomDebugSendFormAsVoice    = "as_voice"
	wecomDebugSendFormReturnPath = "return_path"

	wecomDebugSendKind = "debug_send"

	wecomDebugSendNoticeSent = "WeCom debug message sent."

	wecomDebugSendErrInvalid      = "invalid_request"
	wecomDebugSendErrRuntimeGone  = "runtime_not_found"
	wecomDebugSendErrUnsupported  = "runtime_not_supported"
	wecomDebugSendErrDisconnected = "runtime_not_connected"
	wecomDebugSendErrDelivery     = "delivery_failed"
)

type wecomDebugSendAdminService struct {
	mu sync.Mutex

	runtimes   []*wecomDebugSendRuntime
	index      map[string]*wecomDebugSendRuntime
	byIdentity map[string]*wecomDebugSendRuntime
}

type wecomDebugSendRuntime struct {
	identity      string
	key           string
	title         string
	target        wecomchannel.AdminTarget
	defaultTarget string

	sender         occhannel.MessageSender
	statusProvider wecomAdminActivationStatusProvider
}

type wecomDebugSendRuntimeView struct {
	RuntimeKey    string                  `json:"runtime_key"`
	Title         string                  `json:"title"`
	Name          string                  `json:"name,omitempty"`
	DefaultTarget string                  `json:"default_target,omitempty"`
	Send          wecomActivateStatusView `json:"send"`
}

type wecomDebugSendRequest struct {
	RuntimeKey string `json:"runtime_key"`
	Target     string `json:"target"`
	Text       string `json:"text,omitempty"`
	FilePath   string `json:"file_path,omitempty"`
	FileName   string `json:"file_name,omitempty"`
	AsVoice    bool   `json:"as_voice,omitempty"`
	ReturnPath string `json:"-"`
}

type wecomDebugSendResponse struct {
	OK          bool      `json:"ok"`
	RuntimeKey  string    `json:"runtime_key"`
	Target      string    `json:"target"`
	MessageKind string    `json:"message_kind"`
	SentAt      time.Time `json:"sent_at"`
}

type wecomDebugSendError struct {
	Status  int
	Code    string
	Message string
}

func (e *wecomDebugSendError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

type wecomDebugSendErrorResponse struct {
	OK    bool                    `json:"ok"`
	Error wecomDebugSendErrorBody `json:"error"`
}

type wecomDebugSendErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func newWeComDebugSendAdminService(
	channels []occhannel.Channel,
) *wecomDebugSendAdminService {
	runtimes := collectWeComDebugSendRuntimes(channels)
	return &wecomDebugSendAdminService{
		runtimes:   runtimes,
		index:      map[string]*wecomDebugSendRuntime{},
		byIdentity: map[string]*wecomDebugSendRuntime{},
	}
}

func collectWeComDebugSendRuntimes(
	channels []occhannel.Channel,
) []*wecomDebugSendRuntime {
	runtimes := make([]*wecomDebugSendRuntime, 0, len(channels))
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
		sender, ok := ch.(occhannel.MessageSender)
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
		runtimes = append(runtimes, &wecomDebugSendRuntime{
			identity: identity,
			key:      buildWeComActivateRuntimeKey(target),
			title: channelRuntimeTitle(
				"WeCom Runtime",
				target.Name,
			),
			target:         target,
			defaultTarget:  readWeComDebugSendDefaultTarget(target.StateDir),
			sender:         sender,
			statusProvider: statusProvider,
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

func readWeComDebugSendDefaultTarget(stateDir string) string {
	userID := readWeComActivateDefaultUserID(stateDir)
	if userID == "" {
		return ""
	}
	return wecomchannel.BuildAdminDirectMessageTarget(userID)
}

func (s *wecomDebugSendAdminService) ensureIndex() {
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

func (s *wecomDebugSendAdminService) runtimeViewByIdentity(
	identity string,
) (wecomDebugSendRuntimeView, bool) {
	if s == nil {
		return wecomDebugSendRuntimeView{}, false
	}
	s.ensureIndex()
	runtime := s.byIdentity[strings.TrimSpace(identity)]
	if runtime == nil {
		return wecomDebugSendRuntimeView{}, false
	}
	return runtime.view(), true
}

func (r *wecomDebugSendRuntime) view() wecomDebugSendRuntimeView {
	if r == nil {
		return wecomDebugSendRuntimeView{}
	}
	status := wecomchannel.AdminActivationStatus{}
	if r.statusProvider != nil {
		status = r.statusProvider.WeComAdminActivationStatus()
	}
	return wecomDebugSendRuntimeView{
		RuntimeKey:    strings.TrimSpace(r.key),
		Title:         strings.TrimSpace(r.title),
		Name:          strings.TrimSpace(r.target.Name),
		DefaultTarget: strings.TrimSpace(r.defaultTarget),
		Send:          activationStatusView(status),
	}
}

func wrapWeComDebugSendAdminHandler(
	base http.Handler,
	service *wecomDebugSendAdminService,
) http.Handler {
	if service == nil {
		return base
	}

	mux := http.NewServeMux()
	mux.HandleFunc(
		wecomDebugSendActionPath,
		service.handleSend,
	)
	if base != nil {
		mux.Handle("/", base)
	}
	return mux
}

func (s *wecomDebugSendAdminService) handleSend(
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

	request, jsonMode, err := parseWeComDebugSendRequest(r)
	if err != nil {
		s.writeSendFailure(
			w,
			r,
			jsonMode,
			newWeComDebugSendError(
				http.StatusBadRequest,
				wecomDebugSendErrInvalid,
				"Invalid debug send request.",
			),
		)
		return
	}

	response, sendErr := s.send(r.Context(), request)
	if sendErr != nil {
		s.writeSendFailure(w, r, jsonMode, sendErr)
		return
	}
	s.writeSendSuccess(w, r, jsonMode, request, response)
}

func parseWeComDebugSendRequest(
	r *http.Request,
) (wecomDebugSendRequest, bool, error) {
	if isWeComActivateJSONRequest(r) {
		return parseWeComDebugSendJSONRequest(r)
	}
	request, err := parseWeComDebugSendFormRequest(r)
	return request, false, err
}

func parseWeComDebugSendJSONRequest(
	r *http.Request,
) (wecomDebugSendRequest, bool, error) {
	var request wecomDebugSendRequest
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
	return normalizeWeComDebugSendRequest(request), true, nil
}

func parseWeComDebugSendFormRequest(
	r *http.Request,
) (wecomDebugSendRequest, error) {
	var request wecomDebugSendRequest
	if r == nil {
		return request, errors.New("nil request")
	}
	if err := r.ParseForm(); err != nil {
		return request, err
	}
	request = wecomDebugSendRequest{
		RuntimeKey: r.FormValue(wecomDebugSendFormRuntimeKey),
		Target:     r.FormValue(wecomDebugSendFormTarget),
		Text:       r.FormValue(wecomDebugSendFormText),
		FilePath:   r.FormValue(wecomDebugSendFormFilePath),
		FileName:   r.FormValue(wecomDebugSendFormFileName),
		AsVoice: parseWeComDebugSendBool(r.FormValue(
			wecomDebugSendFormAsVoice,
		)),
		ReturnPath: r.FormValue(wecomDebugSendFormReturnPath),
	}
	return normalizeWeComDebugSendRequest(request), nil
}

func parseWeComDebugSendBool(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "on", "yes":
		return true
	default:
		return false
	}
}

func normalizeWeComDebugSendRequest(
	request wecomDebugSendRequest,
) wecomDebugSendRequest {
	request.RuntimeKey = strings.TrimSpace(request.RuntimeKey)
	request.Target = strings.TrimSpace(request.Target)
	request.Text = strings.TrimSpace(request.Text)
	request.FilePath = strings.TrimSpace(request.FilePath)
	request.FileName = strings.TrimSpace(request.FileName)
	request.ReturnPath = sanitizeWeComActivateReturnPath(
		request.ReturnPath,
	)
	return request
}

func (s *wecomDebugSendAdminService) send(
	ctx context.Context,
	request wecomDebugSendRequest,
) (wecomDebugSendResponse, *wecomDebugSendError) {
	request = normalizeWeComDebugSendRequest(request)
	if request.RuntimeKey == "" || request.Target == "" {
		return wecomDebugSendResponse{}, newWeComDebugSendError(
			http.StatusBadRequest,
			wecomDebugSendErrInvalid,
			"`runtime_key` and `target` are required.",
		)
	}
	msg := buildWeComDebugSendMessage(request)
	if strings.TrimSpace(msg.Text) == "" && len(msg.Files) == 0 {
		return wecomDebugSendResponse{}, newWeComDebugSendError(
			http.StatusBadRequest,
			wecomDebugSendErrInvalid,
			"`text` or `file_path` is required.",
		)
	}

	s.ensureIndex()
	runtime := s.index[request.RuntimeKey]
	if runtime == nil {
		return wecomDebugSendResponse{}, newWeComDebugSendError(
			http.StatusNotFound,
			wecomDebugSendErrRuntimeGone,
			"The selected WeCom runtime was not found.",
		)
	}
	status := runtime.statusProvider.WeComAdminActivationStatus()
	if !status.Supported {
		return wecomDebugSendResponse{}, newWeComDebugSendError(
			http.StatusConflict,
			wecomDebugSendErrUnsupported,
			"The selected WeCom runtime does not support "+
				"debug send.",
		)
	}
	if !status.Available {
		return wecomDebugSendResponse{}, newWeComDebugSendError(
			http.StatusConflict,
			wecomDebugSendErrDisconnected,
			"The selected WeCom runtime is not currently "+
				"connected.",
		)
	}
	if err := runtime.sender.SendMessage(ctx, request.Target, msg); err != nil {
		latest := runtime.statusProvider.WeComAdminActivationStatus()
		if latest.Supported && !latest.Available {
			return wecomDebugSendResponse{}, newWeComDebugSendError(
				http.StatusConflict,
				wecomDebugSendErrDisconnected,
				"The selected WeCom runtime is not currently "+
					"connected.",
			)
		}
		return wecomDebugSendResponse{}, newWeComDebugSendError(
			http.StatusBadGateway,
			wecomDebugSendErrDelivery,
			"Failed to deliver the debug message.",
		)
	}
	return wecomDebugSendResponse{
		OK:          true,
		RuntimeKey:  request.RuntimeKey,
		Target:      request.Target,
		MessageKind: wecomDebugSendKind,
		SentAt:      time.Now(),
	}, nil
}

func buildWeComDebugSendMessage(
	request wecomDebugSendRequest,
) occhannel.OutboundMessage {
	msg := occhannel.OutboundMessage{Text: request.Text}
	if strings.TrimSpace(request.FilePath) == "" {
		return msg
	}
	msg.Files = append(msg.Files, occhannel.OutboundFile{
		Path:    request.FilePath,
		Name:    request.FileName,
		AsVoice: request.AsVoice,
	})
	return msg
}

func (s *wecomDebugSendAdminService) writeSendFailure(
	w http.ResponseWriter,
	r *http.Request,
	jsonMode bool,
	err *wecomDebugSendError,
) {
	if err == nil {
		err = newWeComDebugSendError(
			http.StatusInternalServerError,
			wecomDebugSendErrDelivery,
			http.StatusText(http.StatusInternalServerError),
		)
	}
	if jsonMode {
		writeRuntimeJSON(
			w,
			err.Status,
			wecomDebugSendErrorResponse{
				OK: false,
				Error: wecomDebugSendErrorBody{
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
				r.FormValue(wecomDebugSendFormReturnPath),
			),
			weixinAdminQueryError,
			err.Message,
		),
		http.StatusSeeOther,
	)
}

func (s *wecomDebugSendAdminService) writeSendSuccess(
	w http.ResponseWriter,
	r *http.Request,
	jsonMode bool,
	request wecomDebugSendRequest,
	response wecomDebugSendResponse,
) {
	if jsonMode {
		writeRuntimeJSON(w, http.StatusOK, response)
		return
	}
	writeAdminRedirect(
		w,
		wecomActivateRedirectLocation(
			r.URL.Path,
			request.ReturnPath,
			weixinAdminQueryNotice,
			wecomDebugSendNoticeSent,
		),
		http.StatusSeeOther,
	)
}

func newWeComDebugSendError(
	status int,
	code string,
	message string,
) *wecomDebugSendError {
	return &wecomDebugSendError{
		Status:  status,
		Code:    strings.TrimSpace(code),
		Message: strings.TrimSpace(message),
	}
}
