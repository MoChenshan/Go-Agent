package wecom

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"git.woa.com/trpc-go/trpc-agent-go/openclaw/runtimectl"
	"trpc.group/trpc-go/trpc-agent-go/log"
)

const (
	runtimeCompletionDirName = "runtime_notices"
	runtimeCompletionExtName = ".json"

	defaultRuntimeCompletionTTL = 30 * time.Minute
	runtimeCompletionTimeout    = 5 * time.Second

	runtimeCompletionDonePrefix = "✅ 已完成"
	runtimeCompletionWarnPrefix = "⚠️ 实例已重新拉起，但"
	runtimeCompletionVersion    = "当前版本："
	runtimeCompletionTarget     = "目标版本："
	runtimeCompletionBefore     = "升级前版本："
	runtimeCompletionSummary    = "变更摘要："
	runtimeCompletionUnknown    = "unknown"
	runtimeCompletionRestart    = "重启"
	runtimeCompletionUpgrade    = "升级"
	runtimeCompletionGraceful   = "无损"
	runtimeCompletionForce      = "强制"
	runtimeCompletionFrom       = "已从 "
	runtimeCompletionTo         = " 升级到 "
)

type runtimeCompletionNotice struct {
	ActionID       string                `json:"action_id"`
	Action         runtimectl.ActionKind `json:"action"`
	Mode           runtimectl.ActionMode `json:"mode"`
	TargetVersion  string                `json:"target_version,omitempty"`
	CurrentVersion string                `json:"current_version,omitempty"`
	Actor          string                `json:"actor,omitempty"`
	Source         string                `json:"source,omitempty"`
	Summary        []string              `json:"summary,omitempty"`
	ResponseURL    string                `json:"response_url,omitempty"`
	ReplyReqID     string                `json:"reply_req_id,omitempty"`
	PushTarget     string                `json:"push_target,omitempty"`
	RequestedAt    time.Time             `json:"requested_at"`
	CreatedAt      time.Time             `json:"created_at"`
}

type runtimeCompletionNoticeFile struct {
	path   string
	notice runtimeCompletionNotice
}

type runtimeCompletionNotifier struct {
	mu sync.Mutex

	stateDir string

	ttl         time.Duration
	sendTimeout time.Duration

	currentVersion func() string
	sendResponse   func(context.Context, string, string) error
}

func newRuntimeCompletionNotifier(
	stateDir string,
	currentVersion func() string,
	sendResponse func(context.Context, string, string) error,
) *runtimeCompletionNotifier {
	stateDir = strings.TrimSpace(stateDir)
	if stateDir == "" {
		return nil
	}
	return &runtimeCompletionNotifier{
		stateDir:       stateDir,
		ttl:            defaultRuntimeCompletionTTL,
		sendTimeout:    runtimeCompletionTimeout,
		currentVersion: currentVersion,
		sendResponse:   sendResponse,
	}
}

func (n *runtimeCompletionNotifier) stage(
	result runtimectl.ActionResult,
	pushTarget string,
	responseURL string,
	replyReqID string,
) error {
	if n == nil || !result.Started {
		return nil
	}

	pushTarget = strings.TrimSpace(pushTarget)
	responseURL = strings.TrimSpace(responseURL)
	replyReqID = strings.TrimSpace(replyReqID)
	if pushTarget == "" && responseURL == "" && replyReqID == "" {
		return nil
	}
	if result.Status.Pending == nil {
		return nil
	}

	notice := runtimeCompletionNotice{
		ActionID: strings.TrimSpace(result.Status.Pending.ID),
		Action:   result.Status.Pending.Kind,
		Mode:     result.Status.Pending.Mode,
		TargetVersion: strings.TrimSpace(
			result.Status.Pending.TargetVersion,
		),
		CurrentVersion: strings.TrimSpace(result.Status.CurrentVersion),
		Actor:          strings.TrimSpace(result.Status.Pending.Actor),
		Source:         strings.TrimSpace(result.Status.Pending.Source),
		Summary: append(
			[]string(nil),
			result.Status.Pending.Summary...,
		),
		ResponseURL: responseURL,
		ReplyReqID:  replyReqID,
		PushTarget:  pushTarget,
		RequestedAt: result.Status.Pending.RequestedAt,
		CreatedAt:   time.Now(),
	}
	return writeRuntimeCompletionNotice(
		n.noticePath(notice.ActionID),
		notice,
	)
}

func (n *runtimeCompletionNotifier) flush(
	ctx context.Context,
) {
	if n == nil || n.sendResponse == nil {
		return
	}

	n.mu.Lock()
	defer n.mu.Unlock()

	notices, err := n.loadPending()
	if err != nil {
		log.Warnf(
			"wecom: load runtime completion notices failed: %v",
			err,
		)
		return
	}
	if len(notices) == 0 {
		return
	}

	currentVersion := n.resolveCurrentVersion()
	for _, item := range notices {
		if strings.TrimSpace(item.notice.ResponseURL) == "" {
			log.Warnf(
				"wecom: drop runtime completion notice without "+
					"response_url: action_id=%s",
				item.notice.ActionID,
			)
			if err := os.Remove(item.path); err != nil &&
				!os.IsNotExist(err) {
				log.Warnf(
					"wecom: remove runtime completion notice failed: "+
						"%s: %v",
					item.path,
					err,
				)
			}
			continue
		}
		message := formatRuntimeCompletionMessage(
			item.notice,
			currentVersion,
		)
		sendCtx, cancel := context.WithTimeout(
			ctx,
			n.sendTimeout,
		)
		err := n.sendResponse(
			sendCtx,
			item.notice.ResponseURL,
			message,
		)
		cancel()
		if err != nil {
			log.Warnf(
				"wecom: send runtime completion notice failed: "+
					"action_id=%s response_url=%s err=%v",
				item.notice.ActionID,
				item.notice.ResponseURL,
				err,
			)
			return
		}
		if err := os.Remove(item.path); err != nil &&
			!os.IsNotExist(err) {
			log.Warnf(
				"wecom: remove runtime completion notice failed: "+
					"%s: %v",
				item.path,
				err,
			)
		}
	}
}

func (n *runtimeCompletionNotifier) resolveCurrentVersion() string {
	if n == nil || n.currentVersion == nil {
		return runtimeCompletionUnknown
	}
	value := strings.TrimSpace(n.currentVersion())
	if value == "" {
		return runtimeCompletionUnknown
	}
	return value
}

func (n *runtimeCompletionNotifier) loadPending() (
	[]runtimeCompletionNoticeFile,
	error,
) {
	dir := n.noticeDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	now := time.Now()
	files := make([]runtimeCompletionNoticeFile, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if filepath.Ext(entry.Name()) != runtimeCompletionExtName {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		notice, readErr := readRuntimeCompletionNotice(path)
		if readErr != nil {
			log.Warnf(
				"wecom: drop invalid runtime completion notice: "+
					"%s: %v",
				path,
				readErr,
			)
			_ = os.Remove(path)
			continue
		}
		if shouldExpireRuntimeCompletionNotice(
			notice,
			now,
			n.ttl,
		) {
			_ = os.Remove(path)
			continue
		}
		files = append(files, runtimeCompletionNoticeFile{
			path:   path,
			notice: notice,
		})
	}

	sort.Slice(files, func(i, j int) bool {
		left := files[i].notice.CreatedAt
		right := files[j].notice.CreatedAt
		if left.Equal(right) {
			return files[i].notice.ActionID <
				files[j].notice.ActionID
		}
		return left.Before(right)
	})
	return files, nil
}

func (n *runtimeCompletionNotifier) noticeDir() string {
	return filepath.Join(
		n.stateDir,
		pluginType,
		runtimeCompletionDirName,
	)
}

func (n *runtimeCompletionNotifier) noticePath(actionID string) string {
	actionID = strings.TrimSpace(actionID)
	if actionID == "" {
		actionID = "pending"
	}
	return filepath.Join(
		n.noticeDir(),
		actionID+runtimeCompletionExtName,
	)
}

func writeRuntimeCompletionNotice(
	path string,
	notice runtimeCompletionNotice,
) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(notice, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o600)
}

func readRuntimeCompletionNotice(
	path string,
) (runtimeCompletionNotice, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return runtimeCompletionNotice{}, err
	}
	var notice runtimeCompletionNotice
	if err := json.Unmarshal(data, &notice); err != nil {
		return runtimeCompletionNotice{}, err
	}
	if strings.TrimSpace(notice.ResponseURL) == "" &&
		strings.TrimSpace(notice.ReplyReqID) == "" &&
		strings.TrimSpace(notice.PushTarget) == "" {
		return runtimeCompletionNotice{}, fmt.Errorf(
			"missing completion delivery target",
		)
	}
	if strings.TrimSpace(notice.ActionID) == "" {
		return runtimeCompletionNotice{}, fmt.Errorf(
			"missing action id",
		)
	}
	return notice, nil
}

func shouldExpireRuntimeCompletionNotice(
	notice runtimeCompletionNotice,
	now time.Time,
	ttl time.Duration,
) bool {
	if ttl <= 0 {
		return false
	}
	base := notice.CreatedAt
	if base.IsZero() {
		base = notice.RequestedAt
	}
	if base.IsZero() {
		return true
	}
	return now.Sub(base) > ttl
}

func buildRuntimeCompletionPushTarget(
	chatID string,
	fromID string,
) string {
	chatID = strings.TrimSpace(chatID)
	if chatID != "" {
		return buildPushTarget(pushTargetKindGroup, chatID)
	}

	fromID = strings.TrimSpace(fromID)
	if fromID != "" {
		return buildPushTarget(pushTargetKindSingle, fromID)
	}
	return ""
}

func formatRuntimeCompletionMessage(
	notice runtimeCompletionNotice,
	currentVersion string,
) string {
	previousVersion := strings.TrimSpace(notice.CurrentVersion)
	currentVersion = strings.TrimSpace(currentVersion)
	if currentVersion == "" || currentVersion == runtimeCompletionUnknown {
		currentVersion = previousVersion
	}
	if currentVersion == "" {
		currentVersion = runtimeCompletionUnknown
	}

	if notice.Action == runtimectl.ActionUpgrade {
		targetVersion := strings.TrimSpace(notice.TargetVersion)
		if targetVersion != "" &&
			currentVersion != targetVersion {
			return strings.Join(
				[]string{
					runtimeCompletionWarnPrefix,
					runtimeCompletionBefore +
						defaultRuntimeCompletionVersion(
							previousVersion,
						),
					runtimeCompletionVersion +
						currentVersion,
					runtimeCompletionTarget +
						targetVersion,
					"请检查外层 start.sh。",
				},
				"\n",
			)
		}
	}

	lines := []string{
		runtimeCompletionDonePrefix +
			runtimeCompletionActionText(
				notice.Action,
				notice.Mode,
			),
	}
	if notice.Action == runtimectl.ActionUpgrade {
		lines = append(
			lines,
			runtimeCompletionUpgradeResultLine(
				previousVersion,
				currentVersion,
			),
		)
		targetVersion := strings.TrimSpace(notice.TargetVersion)
		if targetVersion != "" && targetVersion != currentVersion {
			lines = append(
				lines,
				runtimeCompletionTarget+targetVersion,
			)
		}
		if len(notice.Summary) > 0 {
			lines = append(lines, runtimeCompletionSummary)
			for _, note := range notice.Summary {
				note = strings.TrimSpace(note)
				if note == "" {
					continue
				}
				lines = append(lines, "- "+note)
			}
		}
		return strings.Join(lines, "\n")
	}
	lines = append(lines, runtimeCompletionVersion+currentVersion)
	return strings.Join(lines, "\n")
}

func runtimeCompletionUpgradeResultLine(
	previousVersion string,
	currentVersion string,
) string {
	previousVersion = defaultRuntimeCompletionVersion(previousVersion)
	currentVersion = defaultRuntimeCompletionVersion(currentVersion)
	if previousVersion == currentVersion {
		return runtimeCompletionVersion + currentVersion
	}
	return runtimeCompletionFrom + previousVersion +
		runtimeCompletionTo + currentVersion
}

func defaultRuntimeCompletionVersion(version string) string {
	version = strings.TrimSpace(version)
	if version == "" {
		return runtimeCompletionUnknown
	}
	return version
}

func runtimeCompletionActionText(
	action runtimectl.ActionKind,
	mode runtimectl.ActionMode,
) string {
	modeText := runtimeCompletionGraceful
	if mode == runtimectl.ModeForce {
		modeText = runtimeCompletionForce
	}

	actionText := runtimeCompletionRestart
	if action == runtimectl.ActionUpgrade {
		actionText = runtimeCompletionUpgrade
	}
	return modeText + actionText
}

func (c *Channel) stageRuntimeCompletionNotice(
	result runtimectl.ActionResult,
	chatID string,
	fromID string,
	responseURL string,
	replyReqID string,
) {
	if c == nil || c.runtimeCompletionNotifier == nil {
		return
	}
	err := c.runtimeCompletionNotifier.stage(
		result,
		buildRuntimeCompletionPushTarget(chatID, fromID),
		responseURL,
		replyReqID,
	)
	if err != nil {
		log.Warnf(
			"wecom: stage runtime completion notice failed: %v",
			err,
		)
	}
}

func (c *Channel) flushPendingRuntimeCompletionNotices(
	ctx context.Context,
) {
	if c == nil || c.runtimeCompletionNotifier == nil {
		return
	}
	c.runtimeCompletionNotifier.flush(ctx)
}

func (c *Channel) sendRuntimeCompletionResponse(
	ctx context.Context,
	responseURL string,
	message string,
) error {
	if c == nil {
		return fmt.Errorf("wecom: nil channel")
	}
	responseURL = strings.TrimSpace(responseURL)
	if responseURL == "" {
		return fmt.Errorf(
			"wecom: empty runtime completion response_url",
		)
	}
	return newAIBotSender(
		responseURL,
		&http.Client{Timeout: 30 * time.Second},
	).SendMarkdown(
		ctx,
		"",
		message,
	)
}

func runtimeCompletionReplyReqID(
	sender messageSender,
) string {
	replySender, ok := sender.(*aibotWebSocketSender)
	if !ok || replySender == nil {
		return ""
	}
	return strings.TrimSpace(replySender.reqID)
}

func (c *Channel) currentRuntimeVersion() string {
	if c == nil || c.runtimeLifecycle == nil {
		return ""
	}
	return strings.TrimSpace(
		c.runtimeLifecycleStatus().CurrentVersion,
	)
}
