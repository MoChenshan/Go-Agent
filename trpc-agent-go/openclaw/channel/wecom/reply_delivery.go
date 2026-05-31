package wecom

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"trpc.group/trpc-go/trpc-agent-go/log"
)

const (
	replyFileMarkerPrefix = "[WECOM_FILE:"
	replyFileMarkerSuffix = "]"
	replyMediaLinePrefix  = "MEDIA:"

	maxReplyDeliveryFiles = 5
	maxReplyDeliveryNotes = 3

	replyDeliverySuccessMessage = "附件已回传。"
	replyDeliveryPartialMessage = "已回传部分附件。"
	replyDeliveryFailedMessage  = "已生成结果，但当前无法自动回传附件；" +
		"未成功回传的附件引用已移除。"
	replyDeliveryUnverifiedFileMessage = "回复里引用了附件，" +
		"但运行时无法验证对应的实际文件；已阻止自动回传并" +
		"丢弃未验证的附件声明。"
	replyDeliveryReasonHeader = "回传失败原因："
	replyDeliverySenderNote   = "当前发送方式不支持自动回传附件。"
	replyDeliveryNoRootNote   = "当前运行时没有可用的回传目录，" +
		"无法自动回传本地产物。"
	replyDeliveryProgressFmt = "正在回传附件 %d/%d：%s..."

	replyDeliveryOutputDirName = "out"
)

var (
	errReplyDeliveryEmptyPath = errors.New(
		"empty reply delivery path",
	)
	errReplyDeliveryPathMustBeAbs = errors.New(
		"reply delivery path must be absolute",
	)
	errReplyDeliveryPathNotFile = errors.New(
		"reply delivery path is not a file",
	)
	errReplyDeliveryPathSymlink = errors.New(
		"reply delivery path is a symlink",
	)
	errReplyDeliveryPathOutsideRoots = errors.New(
		"reply delivery path outside allowed roots",
	)
	errReplyDeliveryPathAmbiguous = errors.New(
		"reply delivery path matched multiple files",
	)
)

type replyDeliveryIssueCode string

const (
	replyDeliveryIssueUnknown replyDeliveryIssueCode = "unknown"
	replyDeliveryIssueMissing replyDeliveryIssueCode = "missing"
	replyDeliveryIssuePath    replyDeliveryIssueCode = "path"
	replyDeliveryIssueSend    replyDeliveryIssueCode = "send"
	replyDeliveryIssueConfig  replyDeliveryIssueCode = "config"
	replyDeliveryIssueMatch   replyDeliveryIssueCode = "match"
)

type replyDeliveryPlan struct {
	cleanReply string
	paths      []string
	requested  int
	issues     []replyDeliveryIssue
}

type replyDeliveryOutcome struct {
	requested int
	sent      int
	failed    int
	issues    []replyDeliveryIssue
}

type replyDeliveryIssue struct {
	code replyDeliveryIssueCode
	note string
}

type replyDeliveryProgressFunc func(path string, index int, total int)

func stripReplyFileMarkers(text string) string {
	cleaned, _ := parseReplyFileMarkers(text)
	return cleaned
}

func parseReplyFileMarkers(text string) (string, []string) {
	lines := strings.Split(text, "\n")
	cleaned := make([]string, 0, len(lines))
	paths := make([]string, 0, len(lines))
	seen := make(map[string]struct{})

	for _, line := range lines {
		path, ok := parseReplyFileMarkerLine(line)
		if !ok {
			cleaned = append(cleaned, line)
			continue
		}
		if path == "" {
			continue
		}
		if _, exists := seen[path]; exists {
			continue
		}
		seen[path] = struct{}{}
		paths = append(paths, path)
	}

	return strings.TrimSpace(strings.Join(cleaned, "\n")), paths
}

func parseReplyFileMarkerLine(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if strings.HasPrefix(trimmed, replyMediaLinePrefix) {
		path := strings.TrimSpace(
			strings.TrimPrefix(trimmed, replyMediaLinePrefix),
		)
		return path, path != ""
	}
	if !strings.HasPrefix(trimmed, replyFileMarkerPrefix) {
		return "", false
	}
	if !strings.HasSuffix(trimmed, replyFileMarkerSuffix) {
		return "", false
	}

	path := strings.TrimSuffix(trimmed, replyFileMarkerSuffix)
	path = strings.TrimPrefix(path, replyFileMarkerPrefix)
	return strings.TrimSpace(path), true
}

func (c *Channel) buildReplyDeliveryPlan(
	sessionID string,
	reply string,
) replyDeliveryPlan {
	cleanReply, paths := parseReplyFileMarkers(reply)
	plan := replyDeliveryPlan{
		cleanReply: cleanReply,
		requested:  len(paths),
	}
	if len(paths) == 0 {
		return plan
	}

	roots := c.replyDeliveryRoots(sessionID)
	if len(roots) == 0 {
		plan.issues = append(
			plan.issues,
			replyDeliveryIssue{
				code: replyDeliveryIssueConfig,
				note: replyDeliveryNoRootNote,
			},
		)
		return plan
	}

	limited := paths
	if len(limited) > maxReplyDeliveryFiles {
		plan.issues = append(
			plan.issues,
			replyDeliveryIssue{
				code: replyDeliveryIssueConfig,
				note: fmt.Sprintf(
					"本次最多自动回传 %d 个附件，已忽略其余 %d 个。",
					maxReplyDeliveryFiles,
					len(paths)-maxReplyDeliveryFiles,
				),
			},
		)
		limited = limited[:maxReplyDeliveryFiles]
	}
	for _, path := range limited {
		normalized, err := normalizeReplyDeliveryPath(path, roots)
		if err != nil {
			log.Warnf(
				"wecom reply delivery: skip path %q: %v",
				path,
				err,
			)
			plan.issues = append(
				plan.issues,
				replyDeliveryPathIssue(path, err),
			)
			continue
		}
		plan.paths = append(plan.paths, normalized)
	}
	return plan
}

func (c *Channel) replyDeliveryRoots(sessionID string) []string {
	rootSet := make(map[string]struct{})
	roots := make([]string, 0, 3)

	addRoot := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		path = filepath.Clean(path)
		info, err := os.Stat(path)
		if err != nil || !info.IsDir() {
			return
		}
		if _, exists := rootSet[path]; exists {
			return
		}
		rootSet[path] = struct{}{}
		roots = append(roots, path)
	}

	var info *sessionInfo
	if c != nil && c.sessionTracker != nil {
		baseID := baseSessionIDForSession(sessionID)
		info = c.sessionTracker.getOrCreateSession(baseID, 0)
	}

	workspace := ""
	if c != nil {
		workspace = c.effectiveWorkspacePath(info)
	}
	addRoot(workspace)

	facts := describeWorkspace(workspace)
	addRoot(facts.GitRoot)

	if c != nil {
		addRoot(c.codingScratchRoot)
		addRoot(c.codingArtifactOutputRoot)
		addRoot(c.runtimeTempRoot)
		addRoot(c.runtimeManagedUploadsRoot)
		for _, path := range c.runtimeReplyDeliveryRoots {
			addRoot(path)
		}
	}
	return roots
}

func normalizeReplyDeliveryRoots(paths []string) []string {
	if len(paths) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(paths))
	roots := make([]string, 0, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		path = filepath.Clean(path)
		if _, exists := seen[path]; exists {
			continue
		}
		seen[path] = struct{}{}
		roots = append(roots, path)
	}
	return roots
}

func normalizeReplyDeliveryPath(
	path string,
	roots []string,
) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errReplyDeliveryEmptyPath
	}
	if !filepath.IsAbs(path) {
		return "", errReplyDeliveryPathMustBeAbs
	}

	cleaned := filepath.Clean(path)
	normalized, err := validateReplyDeliveryExistingPath(
		cleaned,
		roots,
	)
	if err == nil {
		return normalized, nil
	}
	if !os.IsNotExist(err) {
		return "", err
	}
	if !replyDeliveryPathWithinAnyRoot(cleaned, roots) {
		return "", err
	}

	recovered, recoverErr := resolveMissingReplyDeliveryPath(
		cleaned,
		roots,
	)
	if recoverErr == nil {
		return recovered, nil
	}
	if errors.Is(recoverErr, errReplyDeliveryPathAmbiguous) {
		return "", recoverErr
	}
	return "", err
}

func validateReplyDeliveryExistingPath(
	path string,
	roots []string,
) (string, error) {
	realPath, err := resolveReplyDeliveryExistingFilePath(path)
	if err != nil {
		return "", err
	}

	for _, root := range roots {
		if replyDeliveryRealPathWithinRoot(realPath, root) {
			return realPath, nil
		}
	}
	return "", errReplyDeliveryPathOutsideRoots
}

func replyDeliveryPathWithinAnyRoot(
	path string,
	roots []string,
) bool {
	for _, root := range roots {
		if replyDeliveryPathWithinRoot(path, root) {
			return true
		}
	}
	return false
}

func resolveReplyDeliveryExistingFilePath(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", errReplyDeliveryPathSymlink
	}
	if !info.Mode().IsRegular() {
		return "", errReplyDeliveryPathNotFile
	}

	realPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	info, err = os.Stat(realPath)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errReplyDeliveryPathNotFile
	}
	return realPath, nil
}

func resolveMissingReplyDeliveryPath(
	path string,
	roots []string,
) (string, error) {
	matches := findReplyDeliveryFallbackMatches(path, roots)
	switch len(matches) {
	case 0:
		return "", os.ErrNotExist
	case 1:
		return matches[0], nil
	default:
		return "", errReplyDeliveryPathAmbiguous
	}
}

func findReplyDeliveryFallbackMatches(
	path string,
	roots []string,
) []string {
	candidates := replyDeliveryFallbackCandidates(path, roots)
	if len(candidates) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(candidates))
	matches := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		normalized, err := validateReplyDeliveryExistingPath(
			candidate,
			roots,
		)
		if err != nil {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		matches = append(matches, normalized)
	}
	return matches
}

func replyDeliveryFallbackCandidates(
	path string,
	roots []string,
) []string {
	seen := make(map[string]struct{}, len(roots)*4)
	candidates := make([]string, 0, len(roots)*4)
	addCandidate := func(candidate string) {
		candidate = filepath.Clean(strings.TrimSpace(candidate))
		if candidate == "" || candidate == path {
			return
		}
		if _, ok := seen[candidate]; ok {
			return
		}
		seen[candidate] = struct{}{}
		candidates = append(candidates, candidate)
	}

	baseName := cleanFilename(path)
	relPaths := replyDeliveryRelativeFallbacks(path, roots)
	matchedRoots := replyDeliveryMatchedRoots(path, roots)
	for _, root := range roots {
		if _, ok := matchedRoots[root]; ok {
			continue
		}
		if baseName != "" {
			addCandidate(filepath.Join(root, baseName))
			addCandidate(
				filepath.Join(
					root,
					replyDeliveryOutputDirName,
					baseName,
				),
			)
		}
		for _, relPath := range relPaths {
			addCandidate(filepath.Join(root, relPath))
			addCandidate(
				filepath.Join(
					root,
					replyDeliveryOutputDirName,
					relPath,
				),
			)
		}
	}
	return candidates
}

func replyDeliveryMatchedRoots(
	path string,
	roots []string,
) map[string]struct{} {
	matched := make(map[string]struct{}, len(roots))
	for _, root := range roots {
		if replyDeliveryPathWithinRoot(path, root) {
			matched[root] = struct{}{}
		}
	}
	return matched
}

func replyDeliveryRelativeFallbacks(
	path string,
	roots []string,
) []string {
	seen := make(map[string]struct{}, len(roots))
	relPaths := make([]string, 0, len(roots))
	for _, root := range roots {
		if !replyDeliveryPathWithinRoot(path, root) {
			continue
		}
		relPath, err := filepath.Rel(root, path)
		if err != nil {
			continue
		}
		relPath = filepath.Clean(strings.TrimSpace(relPath))
		if relPath == "" || relPath == "." {
			continue
		}
		if _, ok := seen[relPath]; ok {
			continue
		}
		seen[relPath] = struct{}{}
		relPaths = append(relPaths, relPath)
	}
	return relPaths
}

func pathWithinRoot(path string, root string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	return rel != ".." &&
		!strings.HasPrefix(
			rel,
			".."+string(filepath.Separator),
		)
}

func replyDeliveryPathWithinRoot(path string, root string) bool {
	cleaned := filepath.Clean(strings.TrimSpace(path))
	if cleaned == "" || !pathWithinRoot(cleaned, root) {
		return false
	}

	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return false
	}
	ancestor, err := deepestExistingReplyDeliveryAncestor(cleaned)
	if err != nil {
		return false
	}
	realAncestor, err := filepath.EvalSymlinks(ancestor)
	if err != nil {
		return false
	}
	return pathWithinRoot(realAncestor, realRoot)
}

func replyDeliveryRealPathWithinRoot(
	realPath string,
	root string,
) bool {
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return false
	}
	return pathWithinRoot(realPath, realRoot)
}

func deepestExistingReplyDeliveryAncestor(
	path string,
) (string, error) {
	current := filepath.Clean(strings.TrimSpace(path))
	for {
		if current == "" {
			return "", os.ErrNotExist
		}
		if _, err := os.Lstat(current); err == nil {
			return current, nil
		} else if !os.IsNotExist(err) {
			return "", err
		}

		parent := filepath.Dir(current)
		if parent == current {
			return "", os.ErrNotExist
		}
		current = parent
	}
}

func (c *Channel) sendReplyDeliveryFiles(
	ctx context.Context,
	sender messageSender,
	chatID string,
	plan replyDeliveryPlan,
	progress replyDeliveryProgressFunc,
) replyDeliveryOutcome {
	outcome := replyDeliveryOutcome{
		requested: plan.requested,
		issues:    append([]replyDeliveryIssue(nil), plan.issues...),
	}
	if plan.requested == 0 {
		return outcome
	}

	fileSender, ok := sender.(localFileSender)
	if !ok {
		outcome.failed = outcome.requested
		outcome.issues = append(
			outcome.issues,
			replyDeliveryIssue{
				code: replyDeliveryIssueConfig,
				note: replyDeliverySenderNote,
			},
		)
		return outcome
	}

	outcome.failed = outcome.requested - len(plan.paths)
	for idx, path := range plan.paths {
		if progress != nil {
			progress(path, idx+1, len(plan.paths))
		}
		if err := fileSender.SendLocalFile(ctx, chatID, path); err != nil {
			outcome.failed++
			log.WarnfContext(
				ctx,
				"wecom reply delivery: send path %q failed: %v",
				path,
				err,
			)
			outcome.issues = append(
				outcome.issues,
				replyDeliverySendIssue(path, err),
			)
			continue
		}
		outcome.sent++
	}
	return outcome
}

func finalizeReplyDeliveryText(
	reply string,
	outcome replyDeliveryOutcome,
) string {
	reply = strings.TrimSpace(reply)
	if outcome.requested == 0 {
		return reply
	}

	switch {
	case outcome.sent == 0 && outcome.failed > 0:
		note := replyDeliveryOutcomeNote(
			replyDeliveryFailureSummary(outcome.issues),
			outcome.issues,
		)
		if shouldReplaceReplyForUnverifiedFiles(outcome) {
			return note
		}
		return appendReplyDeliveryNote(reply, note)
	case outcome.sent > 0 && outcome.failed > 0:
		return appendReplyDeliveryNote(
			reply,
			replyDeliveryOutcomeNote(
				replyDeliveryPartialMessage,
				outcome.issues,
			),
		)
	case outcome.sent > 0 && reply == "":
		return replyDeliverySuccessMessage
	default:
		if len(outcome.issues) == 0 {
			return reply
		}
		return appendReplyDeliveryNote(
			reply,
			replyDeliveryOutcomeNote("", outcome.issues),
		)
	}
}

func shouldReplaceReplyForUnverifiedFiles(
	outcome replyDeliveryOutcome,
) bool {
	if outcome.requested == 0 || outcome.sent > 0 || outcome.failed == 0 {
		return false
	}
	return allReplyDeliveryIssuesMatch(
		outcome.issues,
		replyDeliveryIssueMissing,
		replyDeliveryIssueMatch,
	)
}

func replyDeliveryFailureSummary(
	issues []replyDeliveryIssue,
) string {
	if allReplyDeliveryIssuesMatch(
		issues,
		replyDeliveryIssueMissing,
		replyDeliveryIssueMatch,
	) {
		return replyDeliveryUnverifiedFileMessage
	}
	return replyDeliveryFailedMessage
}

func allReplyDeliveryIssuesMatch(
	issues []replyDeliveryIssue,
	allowed ...replyDeliveryIssueCode,
) bool {
	if len(issues) == 0 || len(allowed) == 0 {
		return false
	}

	allowedSet := make(map[replyDeliveryIssueCode]struct{}, len(allowed))
	for _, code := range allowed {
		allowedSet[code] = struct{}{}
	}
	for _, issue := range issues {
		if _, ok := allowedSet[issue.code]; !ok {
			return false
		}
	}
	return true
}

func appendReplyDeliveryNote(
	reply string,
	note string,
) string {
	reply = strings.TrimSpace(reply)
	note = strings.TrimSpace(note)
	switch {
	case note == "":
		return reply
	case reply == "":
		return note
	case strings.Contains(reply, note):
		return reply
	default:
		return reply + "\n\n" + note
	}
}

func replyDeliveryOutcomeNote(
	summary string,
	issues []replyDeliveryIssue,
) string {
	summary = strings.TrimSpace(summary)
	reasons := formatReplyDeliveryIssues(issues)
	switch {
	case summary == "":
		return reasons
	case reasons == "":
		return summary
	default:
		return summary + "\n" + reasons
	}
}

func formatReplyDeliveryIssues(issues []replyDeliveryIssue) string {
	notes := uniqueReplyDeliveryNotes(issues)
	if len(notes) == 0 {
		return ""
	}

	lines := []string{replyDeliveryReasonHeader}
	limit := len(notes)
	if limit > maxReplyDeliveryNotes {
		limit = maxReplyDeliveryNotes
	}
	for i := 0; i < limit; i++ {
		lines = append(
			lines,
			fmt.Sprintf("%d. %s", i+1, notes[i]),
		)
	}
	if len(notes) > limit {
		lines = append(
			lines,
			fmt.Sprintf(
				"另有 %d 个附件问题未展开。",
				len(notes)-limit,
			),
		)
	}
	return strings.Join(lines, "\n")
}

func uniqueReplyDeliveryNotes(issues []replyDeliveryIssue) []string {
	seen := make(map[string]struct{}, len(issues))
	notes := make([]string, 0, len(issues))
	for _, issue := range issues {
		note := strings.TrimSpace(issue.note)
		if note == "" {
			continue
		}
		if _, ok := seen[note]; ok {
			continue
		}
		seen[note] = struct{}{}
		notes = append(notes, note)
	}
	return notes
}

func replyDeliveryPathIssue(
	path string,
	err error,
) replyDeliveryIssue {
	name := cleanFilename(path)
	if name == "" {
		name = "该附件"
	}
	switch {
	case os.IsNotExist(err):
		return replyDeliveryIssue{
			code: replyDeliveryIssueMissing,
			note: "未找到要回传的文件：" + name + "。",
		}
	case errors.Is(err, errReplyDeliveryPathMustBeAbs):
		return replyDeliveryIssue{
			code: replyDeliveryIssuePath,
			note: "回传文件路径必须是绝对路径：" + name + "。",
		}
	case errors.Is(err, errReplyDeliveryPathNotFile):
		return replyDeliveryIssue{
			code: replyDeliveryIssuePath,
			note: "待回传路径不是普通文件：" + name + "。",
		}
	case errors.Is(err, errReplyDeliveryPathSymlink):
		return replyDeliveryIssue{
			code: replyDeliveryIssuePath,
			note: "待回传文件不能是符号链接：" + name + "。",
		}
	case errors.Is(err, errReplyDeliveryPathOutsideRoots):
		return replyDeliveryIssue{
			code: replyDeliveryIssuePath,
			note: "待回传文件不在当前会话允许的回传目录中：" +
				name + "。",
		}
	case errors.Is(err, errReplyDeliveryPathAmbiguous):
		return replyDeliveryIssue{
			code: replyDeliveryIssueMatch,
			note: "找到多个同名待回传文件，无法确定该发送哪一个：" +
				name + "。",
		}
	default:
		return replyDeliveryIssue{
			code: replyDeliveryIssueUnknown,
			note: "无法访问待回传文件：" + name + "。",
		}
	}
}

func replyDeliverySendIssue(
	path string,
	err error,
) replyDeliveryIssue {
	if issue := replyDeliveryMediaIssue(err); issue.note != "" {
		return issue
	}
	name := cleanFilename(path)
	if name == "" {
		name = "该附件"
	}
	return replyDeliveryIssue{
		code: replyDeliveryIssueSend,
		note: "回传附件失败：" + name + "。",
	}
}

func replyDeliveryMediaIssue(err error) replyDeliveryIssue {
	var limitErr *replyMediaLimitError
	if errors.As(err, &limitErr) {
		return replyDeliveryIssue{
			code: replyDeliveryIssueSend,
			note: limitErr.UserNote(),
		}
	}
	var emptyErr *replyMediaEmptyError
	if errors.As(err, &emptyErr) {
		return replyDeliveryIssue{
			code: replyDeliveryIssueSend,
			note: emptyErr.UserNote(),
		}
	}
	var chunkErr *replyMediaChunkError
	if errors.As(err, &chunkErr) {
		return replyDeliveryIssue{
			code: replyDeliveryIssueSend,
			note: chunkErr.UserNote(),
		}
	}
	return replyDeliveryIssue{}
}

func replyDeliveryProgressText(
	path string,
	index int,
	total int,
) string {
	name := cleanFilename(path)
	if name == "" {
		name = defaultAttachmentName
	}
	return fmt.Sprintf(replyDeliveryProgressFmt, index, total, name)
}
