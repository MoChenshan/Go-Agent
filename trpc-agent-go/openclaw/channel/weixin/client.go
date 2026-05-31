package weixin

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	defaultBaseURL = "https://ilinkai.weixin.qq.com"

	headerContentType      = "Content-Type"
	headerAuthorization    = "Authorization"
	headerAuthorizationKey = "AuthorizationType"
	headerWeChatUIN        = "X-WECHAT-UIN"

	contentTypeJSON = "application/json"
	authTypeBot     = "ilink_bot_token"

	defaultChannelVersion = "trpc-agent-go-openclaw-weixin"

	defaultPollTimeout        = 35 * time.Second
	defaultPollTimeoutGrace   = 5 * time.Second
	defaultAPIRequestTimeout  = 15 * time.Second
	defaultConfigRequestTimer = 10 * time.Second

	defaultPauseDuration = time.Hour

	loginStatusWait              = "wait"
	loginStatusScanned           = "scaned"
	loginStatusConfirmed         = "confirmed"
	loginStatusExpired           = "expired"
	loginStatusScannedRedirected = "scaned_but_redirect"

	endpointGetBotQRCode  = "ilink/bot/get_bot_qrcode"
	endpointQRCodeStatus  = "ilink/bot/get_qrcode_status"
	endpointGetUpdates    = "ilink/bot/getupdates"
	endpointSendMessage   = "ilink/bot/sendmessage"
	endpointGetConfig     = "ilink/bot/getconfig"
	endpointSendTyping    = "ilink/bot/sendtyping"
	defaultLoginBotType   = "3"
	sessionExpiredErrCode = -14

	messageTypeUser = 1
	messageTypeBot  = 2

	messageStateNew        = 0
	messageStateGenerating = 1
	messageStateFinish     = 2

	messageItemTypeText  = 1
	messageItemTypeImage = 2
	messageItemTypeVoice = 3
	messageItemTypeFile  = 4
	messageItemTypeVideo = 5

	typingStatusActive = 1
	typingStatusCancel = 2

	urlSchemeHTTP  = "http"
	urlSchemeHTTPS = "https"
)

type apiClient struct {
	client         *http.Client
	channelVersion string
}

type apiBaseInfo struct {
	ChannelVersion string `json:"channel_version,omitempty"`
}

type apiErrorResponse struct {
	Ret     int    `json:"ret,omitempty"`
	ErrCode int    `json:"errcode,omitempty"`
	ErrMsg  string `json:"errmsg,omitempty"`
}

type apiResponseError struct {
	Endpoint   string
	StatusCode int
	Ret        int
	ErrCode    int
	ErrMsg     string
}

func (e *apiResponseError) Error() string {
	if e == nil {
		return "weixin api: unknown error"
	}
	switch {
	case e.StatusCode > 0 && e.ErrCode != 0:
		return fmt.Sprintf(
			"weixin api %s: status=%d errcode=%d errmsg=%s",
			e.Endpoint,
			e.StatusCode,
			e.ErrCode,
			e.ErrMsg,
		)
	case e.StatusCode > 0:
		return fmt.Sprintf(
			"weixin api %s: status=%d",
			e.Endpoint,
			e.StatusCode,
		)
	case e.ErrCode != 0:
		return fmt.Sprintf(
			"weixin api %s: errcode=%d errmsg=%s",
			e.Endpoint,
			e.ErrCode,
			e.ErrMsg,
		)
	default:
		return fmt.Sprintf(
			"weixin api %s: ret=%d errmsg=%s",
			e.Endpoint,
			e.Ret,
			e.ErrMsg,
		)
	}
}

func newAPIClient(client *http.Client) *apiClient {
	if client == nil {
		client = &http.Client{
			Timeout: defaultPollTimeout + defaultPollTimeoutGrace,
		}
	}
	return &apiClient{
		client:         client,
		channelVersion: defaultChannelVersion,
	}
}

func isSessionExpiredError(err error) bool {
	var apiErr *apiResponseError
	if !errors.As(err, &apiErr) {
		return false
	}
	return apiErr.ErrCode == sessionExpiredErrCode
}

type loginQRCodeResponse struct {
	QRCode    string `json:"qrcode,omitempty"`
	QRCodeURL string `json:"qrcode_img_content,omitempty"`
}

type loginQRCodeStatusResponse struct {
	Status       string `json:"status,omitempty"`
	BotToken     string `json:"bot_token,omitempty"`
	AccountID    string `json:"ilink_bot_id,omitempty"`
	BaseURL      string `json:"baseurl,omitempty"`
	UserID       string `json:"ilink_user_id,omitempty"`
	RedirectHost string `json:"redirect_host,omitempty"`
}

type getUpdatesResponse struct {
	apiErrorResponse

	Messages           []weixinMessage `json:"msgs,omitempty"`
	GetUpdatesBuf      string          `json:"get_updates_buf,omitempty"`
	LongPollingTimeout int             `json:"longpolling_timeout_ms,omitempty"`
}

type getConfigResponse struct {
	apiErrorResponse
	TypingTicket string `json:"typing_ticket,omitempty"`
}

type sendMessageResponse struct {
	apiErrorResponse
}

type sendTypingResponse struct {
	apiErrorResponse
}

type weixinMessage struct {
	Seq          int64         `json:"seq,omitempty"`
	MessageID    int64         `json:"message_id,omitempty"`
	FromUserID   string        `json:"from_user_id,omitempty"`
	ToUserID     string        `json:"to_user_id,omitempty"`
	ClientID     string        `json:"client_id,omitempty"`
	CreateTimeMS int64         `json:"create_time_ms,omitempty"`
	SessionID    string        `json:"session_id,omitempty"`
	MessageType  int           `json:"message_type,omitempty"`
	MessageState int           `json:"message_state,omitempty"`
	ItemList     []messageItem `json:"item_list,omitempty"`
	ContextToken string        `json:"context_token,omitempty"`
}

type messageItem struct {
	Type      int        `json:"type,omitempty"`
	TextItem  *textItem  `json:"text_item,omitempty"`
	VoiceItem *voiceItem `json:"voice_item,omitempty"`
}

type textItem struct {
	Text string `json:"text,omitempty"`
}

type voiceItem struct {
	Text string `json:"text,omitempty"`
}

type sendMessageRequest struct {
	Message  outboundMessage `json:"msg"`
	BaseInfo apiBaseInfo     `json:"base_info,omitempty"`
}

type outboundMessage struct {
	FromUserID   string        `json:"from_user_id,omitempty"`
	ToUserID     string        `json:"to_user_id,omitempty"`
	ClientID     string        `json:"client_id,omitempty"`
	MessageType  int           `json:"message_type,omitempty"`
	MessageState int           `json:"message_state,omitempty"`
	ItemList     []messageItem `json:"item_list,omitempty"`
	ContextToken string        `json:"context_token,omitempty"`
}

type getConfigRequest struct {
	ILinkUserID  string      `json:"ilink_user_id,omitempty"`
	ContextToken string      `json:"context_token,omitempty"`
	BaseInfo     apiBaseInfo `json:"base_info,omitempty"`
}

type sendTypingRequest struct {
	ILinkUserID  string      `json:"ilink_user_id,omitempty"`
	TypingTicket string      `json:"typing_ticket,omitempty"`
	Status       int         `json:"status,omitempty"`
	BaseInfo     apiBaseInfo `json:"base_info,omitempty"`
}

func (c *apiClient) startLoginQR(
	ctx context.Context,
	baseURL string,
	botType string,
) (loginQRCodeResponse, error) {
	if strings.TrimSpace(botType) == "" {
		botType = defaultLoginBotType
	}
	endpoint := endpointGetBotQRCode + "?bot_type=" +
		url.QueryEscape(strings.TrimSpace(botType))
	var rsp loginQRCodeResponse
	if err := c.getJSON(ctx, baseURL, endpoint, &rsp); err != nil {
		return loginQRCodeResponse{}, err
	}
	return rsp, nil
}

func (c *apiClient) pollLoginQR(
	ctx context.Context,
	baseURL string,
	qrcode string,
) (loginQRCodeStatusResponse, error) {
	endpoint := endpointQRCodeStatus + "?qrcode=" +
		url.QueryEscape(strings.TrimSpace(qrcode))
	var rsp loginQRCodeStatusResponse
	if err := c.getJSON(ctx, baseURL, endpoint, &rsp); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return loginQRCodeStatusResponse{
				Status: loginStatusWait,
			}, nil
		}
		return loginQRCodeStatusResponse{}, err
	}
	return rsp, nil
}

func (c *apiClient) getUpdates(
	ctx context.Context,
	account Account,
	cursor string,
	timeout time.Duration,
) (getUpdatesResponse, error) {
	if timeout <= 0 {
		timeout = defaultPollTimeout
	}
	reqCtx, cancel := context.WithTimeout(
		ctx,
		timeout+defaultPollTimeoutGrace,
	)
	defer cancel()

	body := struct {
		GetUpdatesBuf string      `json:"get_updates_buf,omitempty"`
		BaseInfo      apiBaseInfo `json:"base_info,omitempty"`
	}{
		GetUpdatesBuf: strings.TrimSpace(cursor),
		BaseInfo: apiBaseInfo{
			ChannelVersion: c.channelVersion,
		},
	}

	var rsp getUpdatesResponse
	err := c.postJSON(
		reqCtx,
		account.effectiveBaseURL(defaultBaseURL),
		account.Token,
		endpointGetUpdates,
		body,
		&rsp,
	)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return getUpdatesResponse{
				apiErrorResponse: apiErrorResponse{Ret: 0},
				GetUpdatesBuf:    strings.TrimSpace(cursor),
			}, nil
		}
		return getUpdatesResponse{}, err
	}
	return rsp, nil
}

func (c *apiClient) sendText(
	ctx context.Context,
	account Account,
	peerID string,
	contextToken string,
	clientID string,
	text string,
) error {
	items := make([]messageItem, 0, 1)
	if strings.TrimSpace(text) != "" {
		items = append(items, messageItem{
			Type: messageItemTypeText,
			TextItem: &textItem{
				Text: text,
			},
		})
	}

	body := sendMessageRequest{
		Message: outboundMessage{
			ToUserID:     strings.TrimSpace(peerID),
			ClientID:     strings.TrimSpace(clientID),
			MessageType:  messageTypeBot,
			MessageState: messageStateFinish,
			ItemList:     items,
			ContextToken: strings.TrimSpace(contextToken),
		},
		BaseInfo: apiBaseInfo{
			ChannelVersion: c.channelVersion,
		},
	}

	var rsp sendMessageResponse
	if err := c.postJSON(
		ctx,
		account.effectiveBaseURL(defaultBaseURL),
		account.Token,
		endpointSendMessage,
		body,
		&rsp,
	); err != nil {
		return err
	}
	return validateAPIResponse(endpointSendMessage, rsp.apiErrorResponse)
}

func (c *apiClient) getTypingTicket(
	ctx context.Context,
	account Account,
	peerID string,
	contextToken string,
) (string, error) {
	body := getConfigRequest{
		ILinkUserID:  strings.TrimSpace(peerID),
		ContextToken: strings.TrimSpace(contextToken),
		BaseInfo: apiBaseInfo{
			ChannelVersion: c.channelVersion,
		},
	}

	var rsp getConfigResponse
	if err := c.postJSON(
		ctx,
		account.effectiveBaseURL(defaultBaseURL),
		account.Token,
		endpointGetConfig,
		body,
		&rsp,
	); err != nil {
		return "", err
	}
	if err := validateAPIResponse(
		endpointGetConfig,
		rsp.apiErrorResponse,
	); err != nil {
		return "", err
	}
	return strings.TrimSpace(rsp.TypingTicket), nil
}

func (c *apiClient) sendTypingStatus(
	ctx context.Context,
	account Account,
	peerID string,
	typingTicket string,
	status int,
) error {
	body := sendTypingRequest{
		ILinkUserID:  strings.TrimSpace(peerID),
		TypingTicket: strings.TrimSpace(typingTicket),
		Status:       status,
		BaseInfo: apiBaseInfo{
			ChannelVersion: c.channelVersion,
		},
	}

	var rsp sendTypingResponse
	if err := c.postJSON(
		ctx,
		account.effectiveBaseURL(defaultBaseURL),
		account.Token,
		endpointSendTyping,
		body,
		&rsp,
	); err != nil {
		return err
	}
	return validateAPIResponse(endpointSendTyping, rsp.apiErrorResponse)
}

func validateAPIResponse(
	endpoint string,
	rsp apiErrorResponse,
) error {
	if rsp.ErrCode == 0 && rsp.Ret == 0 {
		return nil
	}
	return &apiResponseError{
		Endpoint: endpoint,
		Ret:      rsp.Ret,
		ErrCode:  rsp.ErrCode,
		ErrMsg:   strings.TrimSpace(rsp.ErrMsg),
	}
}

func (c *apiClient) getJSON(
	ctx context.Context,
	baseURL string,
	endpoint string,
	out any,
) error {
	reqURL, err := buildEndpointURL(baseURL, endpoint)
	if err != nil {
		return fmt.Errorf(
			"weixin api %s: build endpoint url: %w",
			endpoint,
			err,
		)
	}
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		reqURL,
		nil,
	)
	if err != nil {
		return fmt.Errorf("weixin api %s: build request: %w", endpoint, err)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("weixin api %s: do request: %w", endpoint, err)
	}
	defer func() { _ = resp.Body.Close() }()

	return decodeHTTPResponse(endpoint, resp, out)
}

func (c *apiClient) postJSON(
	ctx context.Context,
	baseURL string,
	token string,
	endpoint string,
	body any,
	out any,
) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf(
			"weixin api %s: marshal request: %w",
			endpoint,
			err,
		)
	}

	reqURL, err := buildEndpointURL(baseURL, endpoint)
	if err != nil {
		return fmt.Errorf(
			"weixin api %s: build endpoint url: %w",
			endpoint,
			err,
		)
	}
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		reqURL,
		bytes.NewReader(payload),
	)
	if err != nil {
		return fmt.Errorf("weixin api %s: build request: %w", endpoint, err)
	}
	req.Header.Set(headerContentType, contentTypeJSON)
	req.Header.Set(headerAuthorizationKey, authTypeBot)
	req.Header.Set(headerWeChatUIN, randomWeChatUIN())
	if strings.TrimSpace(token) != "" {
		req.Header.Set(
			headerAuthorization,
			"Bearer "+strings.TrimSpace(token),
		)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("weixin api %s: do request: %w", endpoint, err)
	}
	defer func() { _ = resp.Body.Close() }()

	return decodeHTTPResponse(endpoint, resp, out)
}

func decodeHTTPResponse(
	endpoint string,
	resp *http.Response,
	out any,
) error {
	if resp == nil {
		return &apiResponseError{Endpoint: endpoint}
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("weixin api %s: read body: %w", endpoint, err)
	}

	if resp.StatusCode != http.StatusOK {
		var apiErr apiErrorResponse
		_ = json.Unmarshal(body, &apiErr)
		return &apiResponseError{
			Endpoint:   endpoint,
			StatusCode: resp.StatusCode,
			Ret:        apiErr.Ret,
			ErrCode:    apiErr.ErrCode,
			ErrMsg:     strings.TrimSpace(apiErr.ErrMsg),
		}
	}

	if out == nil || len(bytes.TrimSpace(body)) == 0 {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf(
			"weixin api %s: decode response: %w",
			endpoint,
			err,
		)
	}
	return nil
}

func buildEndpointURL(baseURL string, endpoint string) (string, error) {
	base := strings.TrimSpace(baseURL)
	if base == "" {
		base = defaultBaseURL
	}
	if !strings.HasSuffix(base, "/") {
		base += "/"
	}
	baseParsed, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("invalid base url: %w", err)
	}
	if err := validateEndpointBaseURL(baseParsed); err != nil {
		return "", err
	}

	endpointParsed, err := parseRelativeEndpoint(endpoint)
	if err != nil {
		return "", err
	}
	return baseParsed.ResolveReference(endpointParsed).String(), nil
}

func validateEndpointBaseURL(base *url.URL) error {
	if base == nil {
		return fmt.Errorf("empty base url")
	}
	switch base.Scheme {
	case urlSchemeHTTP, urlSchemeHTTPS:
	default:
		return fmt.Errorf("unsupported base url scheme %q", base.Scheme)
	}
	if base.Host == "" {
		return fmt.Errorf("base url missing host")
	}
	if base.User != nil {
		return fmt.Errorf("base url must not contain user info")
	}
	base.RawQuery = ""
	base.Fragment = ""
	return nil
}

func parseRelativeEndpoint(endpoint string) (*url.URL, error) {
	raw := strings.TrimSpace(endpoint)
	if raw == "" {
		return nil, fmt.Errorf("empty endpoint")
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid endpoint: %w", err)
	}
	if parsed.IsAbs() || parsed.Host != "" {
		return nil, fmt.Errorf("endpoint must be relative")
	}
	parsed.Path = strings.TrimLeft(parsed.Path, "/")
	parsed.Fragment = ""
	return parsed, nil
}

func randomWeChatUIN() string {
	var buf [4]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return base64.StdEncoding.EncodeToString([]byte("0"))
	}
	value := binary.BigEndian.Uint32(buf[:])
	return base64.StdEncoding.EncodeToString(
		[]byte(fmt.Sprintf("%d", value)),
	)
}
