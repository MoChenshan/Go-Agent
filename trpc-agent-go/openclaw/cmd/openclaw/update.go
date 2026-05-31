package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tlog "git.code.oa.com/trpc-go/trpc-go/log"
	"git.woa.com/trpc-go/trpc-agent-go/openclaw/releaseinfo"
)

const (
	defaultReleaseBaseURL  = releaseinfo.DefaultBaseURL
	latestVersionRelPath   = releaseinfo.LatestVersionRelPath
	latestChangelogRelPath = releaseinfo.LatestChangelogRelPath
	upgradeShellName       = "bash"
	channelInstallName     = "install.sh"

	upgradeCheckDisableEnvName = "TRPC_CLAW_DISABLE_UPGRADE_CHECK"

	upgradeFlagVersion          = "version"
	upgradeFlagChannel          = "channel"
	upgradeFlagProfile          = "profile"
	upgradeFlagForceConfig      = "force-config"
	upgradeFlagForceConfigShort = "f"
	upgradeProfileMock          = "mock"
	upgradeProfileWeComAI       = "wecom-ai"
	upgradeProfileWeComWS       = "wecom-ai-websocket"
	upgradeProfileNotify        = "wecom-notification"
	upgradeProfileWeixin        = "weixin"

	upgradeFlagForceConfigLongToken = "--" +
		upgradeFlagForceConfig
	upgradeFlagForceConfigShortToken = "-" +
		upgradeFlagForceConfigShort
	upgradeFlagForceConfigHelp = upgradeFlagForceConfigShortToken +
		", " + upgradeFlagForceConfigLongToken
	upgradeFlagForceConfigUsage = "[" +
		upgradeFlagForceConfigShortToken + "|" +
		upgradeFlagForceConfigLongToken + "]"
	upgradeFlagChannelUsage = "[--channel latest|preview]"
	upgradeFlagVersionUsage = "[--version <tag>]"
	upgradeVersionExample   = "trpc-claw upgrade --version v0.0.46"

	upgradeCheckTimeout   = 3 * time.Second
	upgradeCommandTimeout = 10 * time.Minute

	upgradeRecentChangesLimit = 3

	changelogBulletPrefix = "- "
)

var (
	releaseBaseURL     = defaultReleaseBaseURL
	executablePathFunc = os.Executable
	userHomeDirFunc    = os.UserHomeDir
	tempDirFunc        = os.TempDir
	installReleaseFunc = installRelease
)

type upgradeOptions struct {
	Version     string
	Channel     string
	Profile     string
	ForceConfig bool
}

type upgradeSuggestion struct {
	CurrentVersion string
	LatestVersion  string
	RecentChanges  []string
}

func isTopLevelUpgradeRequest(args []string) bool {
	if len(args) == 0 {
		return false
	}

	return strings.TrimSpace(args[0]) == subcmdUpgrade
}

func runUpgradeCommand(args []string) int {
	if len(args) > 0 && isTopLevelHelpRequest(args) {
		printUpgradeUsage(os.Stdout)
		return 0
	}

	normalized, paths, err := normalizeOpenClawArgsWithPaths(args)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		return 2
	}
	opts, err := parseUpgradeArgs(normalized)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		return 2
	}

	ctx, cancel := context.WithTimeout(
		context.Background(),
		upgradeCommandTimeout,
	)
	defer cancel()

	if err := upgradeRelease(
		ctx,
		os.Stdout,
		os.Stderr,
		paths,
		opts,
	); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func printUpgradeUsage(w io.Writer) {
	_, _ = fmt.Fprintln(w, "Usage:")
	_, _ = fmt.Fprintln(
		w,
		"  trpc-claw upgrade "+upgradeFlagVersionUsage+" "+
			upgradeFlagChannelUsage+" "+
			upgradeFlagForceConfigUsage+
			" [--profile <name>]",
	)
	_, _ = fmt.Fprintln(w, "")
	_, _ = fmt.Fprintln(
		w,
		"Upgrade the current trpc-claw binary to the latest "+
			"release, or install a specific release via "+
			"`--version`.",
	)
	_, _ = fmt.Fprintln(
		w,
		"`"+upgradeFlagForceConfigHelp+"` will also overwrite "+
			"openclaw.yaml and trpc_go.yaml.",
	)
	_, _ = fmt.Fprintln(
		w,
		"`--profile` only takes effect together with "+
			upgradeFlagForceConfigHelp+".",
	)
	_, _ = fmt.Fprintln(
		w,
		"`--version` pins the target release instead of "+
			"resolving `latest/VERSION`.",
	)
	_, _ = fmt.Fprintln(
		w,
		"`--channel preview` resolves `preview/VERSION`; "+
			"the default channel remains latest.",
	)
}

func validateUpgradeArgs(args []string) error {
	_, err := parseUpgradeArgs(args)
	return err
}

func parseUpgradeArgs(args []string) (upgradeOptions, error) {
	var opts upgradeOptions

	for i := 0; i < len(args); i++ {
		raw := strings.TrimSpace(args[i])
		if raw == "" {
			continue
		}

		if value, ok := matchFlagValue(raw, flagConfig); ok {
			if value == "" && isSeparateFlagToken(raw, flagConfig) {
				if i+1 >= len(args) {
					return upgradeOptions{}, fmt.Errorf(
						"flag %q requires a value",
						flagConfig,
					)
				}
				i++
			}
			continue
		}
		if value, ok := matchFlagValue(raw, flagStateDir); ok {
			if value == "" && isSeparateFlagToken(raw, flagStateDir) {
				if i+1 >= len(args) {
					return upgradeOptions{}, fmt.Errorf(
						"flag %q requires a value",
						flagStateDir,
					)
				}
				i++
			}
			continue
		}
		if value, ok := matchFlagValue(raw, upgradeFlagVersion); ok {
			if value == "" {
				if !isSeparateFlagToken(raw, upgradeFlagVersion) {
					return upgradeOptions{}, fmt.Errorf(
						"flag %q requires a value",
						upgradeFlagVersion,
					)
				}
				if i+1 >= len(args) {
					return upgradeOptions{}, fmt.Errorf(
						"flag %q requires a value",
						upgradeFlagVersion,
					)
				}
				value = args[i+1]
				i++
			}
			opts.Version = strings.TrimSpace(value)
			if opts.Version == "" {
				return upgradeOptions{}, fmt.Errorf(
					"flag %q requires a value",
					upgradeFlagVersion,
				)
			}
			continue
		}
		if value, ok := matchFlagValue(raw, upgradeFlagChannel); ok {
			if value == "" {
				if !isSeparateFlagToken(raw, upgradeFlagChannel) {
					return upgradeOptions{}, fmt.Errorf(
						"flag %q requires a value",
						upgradeFlagChannel,
					)
				}
				if i+1 >= len(args) {
					return upgradeOptions{}, fmt.Errorf(
						"flag %q requires a value",
						upgradeFlagChannel,
					)
				}
				value = args[i+1]
				i++
			}
			channel, err := releaseinfo.NormalizeChannel(value)
			if err != nil {
				return upgradeOptions{}, err
			}
			opts.Channel = channel
			continue
		}
		if value, ok := matchFlagValue(raw, upgradeFlagProfile); ok {
			if value == "" {
				if !isSeparateFlagToken(raw, upgradeFlagProfile) {
					return upgradeOptions{}, fmt.Errorf(
						"flag %q requires a value",
						upgradeFlagProfile,
					)
				}
				if i+1 >= len(args) {
					return upgradeOptions{}, fmt.Errorf(
						"flag %q requires a value",
						upgradeFlagProfile,
					)
				}
				value = args[i+1]
				i++
			}
			opts.Profile = strings.TrimSpace(value)
			if opts.Profile == "" {
				return upgradeOptions{}, fmt.Errorf(
					"flag %q requires a value",
					upgradeFlagProfile,
				)
			}
			if !isSupportedUpgradeProfile(opts.Profile) {
				return upgradeOptions{}, fmt.Errorf(
					"unsupported upgrade profile %q",
					opts.Profile,
				)
			}
			continue
		}
		if isShortOrLongFlagToken(
			raw,
			upgradeFlagForceConfigShort,
			upgradeFlagForceConfig,
		) || isBooleanFlagToken(raw, upgradeFlagForceConfig) {
			opts.ForceConfig = true
			continue
		}

		return upgradeOptions{}, fmt.Errorf(
			"upgrade does not support argument %q",
			raw,
		)
	}
	if opts.Profile != "" && !opts.ForceConfig {
		return upgradeOptions{}, fmt.Errorf(
			"upgrade flag %q requires %s",
			upgradeFlagProfile,
			upgradeFlagForceConfigHelp,
		)
	}
	return opts, nil
}

func isShortOrLongFlagToken(
	raw string,
	shortName string,
	longName string,
) bool {
	raw = strings.TrimSpace(raw)
	return raw == "-"+shortName || raw == "--"+longName
}

func isSeparateFlagToken(raw string, name string) bool {
	raw = strings.TrimSpace(raw)
	return raw == "-"+name || raw == "--"+name
}

func isBooleanFlagToken(raw string, name string) bool {
	return isSeparateFlagToken(raw, name)
}

func isSupportedUpgradeProfile(profile string) bool {
	switch strings.TrimSpace(profile) {
	case upgradeProfileMock,
		upgradeProfileWeComAI,
		upgradeProfileWeComWS,
		upgradeProfileNotify,
		upgradeProfileWeixin:
		return true
	default:
		return false
	}
}

func maybeSuggestUpgrade() {
	if upgradeCheckDisabled() {
		return
	}

	ctx, cancel := context.WithTimeout(
		context.Background(),
		upgradeCheckTimeout,
	)
	defer cancel()

	suggestion, ok, err := lookupUpgradeSuggestion(ctx)
	if err != nil || !ok {
		return
	}
	tlog.Infof("%s", buildUpgradeSuggestionText(suggestion))
}

func upgradeCheckDisabled() bool {
	switch strings.ToLower(
		strings.TrimSpace(os.Getenv(upgradeCheckDisableEnvName)),
	) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func upgradeRelease(
	ctx context.Context,
	stdout io.Writer,
	stderr io.Writer,
	paths startupPaths,
	opts upgradeOptions,
) error {
	target, err := resolveUpgradeTargetVersion(ctx, opts)
	if err != nil {
		return err
	}

	current := currentVersion()
	targetVersion := target.Version
	if target.Pinned {
		if releaseinfo.CompareVersions(targetVersion, current) == 0 {
			if !opts.ForceConfig {
				_, _ = fmt.Fprintf(
					stdout,
					"trpc-claw is already at requested version (%s)\n",
					current,
				)
				return nil
			}
			_, _ = fmt.Fprintf(
				stdout,
				"trpc-claw is already at requested version (%s)\n",
				current,
			)
			_, _ = fmt.Fprintln(
				stdout,
				"Reapplying install script because --force-config was set",
			)
		}
	} else if !releaseinfo.HasNewerRelease(
		targetVersion,
		current,
	) {
		if !opts.ForceConfig {
			_, _ = fmt.Fprintf(
				stdout,
				"trpc-claw is already up to date (%s)\n",
				current,
			)
			return nil
		}
		targetVersion = current
		_, _ = fmt.Fprintf(
			stdout,
			"trpc-claw is already up to date (%s)\n",
			current,
		)
		_, _ = fmt.Fprintln(
			stdout,
			"Reapplying install script because --force-config was set",
		)
	}

	binDir, err := currentBinaryDir()
	if err != nil {
		return err
	}
	configDir, err := resolveUpgradeConfigDir(paths)
	if err != nil {
		return err
	}

	if target.Pinned {
		_, _ = fmt.Fprintf(
			stdout,
			"Installing requested trpc-claw version %s "+
				"(current %s)\n",
			targetVersion,
			current,
		)
	} else {
		_, _ = fmt.Fprintf(
			stdout,
			"Upgrading trpc-claw from %s to %s\n",
			current,
			targetVersion,
		)
	}

	if err := installReleaseFunc(
		ctx,
		targetVersion,
		binDir,
		configDir,
		opts,
		stdout,
		stderr,
	); err != nil {
		return fmt.Errorf("upgrade failed: %w", err)
	}
	return nil
}

type upgradeTargetVersion struct {
	Version string
	Pinned  bool
}

func resolveUpgradeTargetVersion(
	ctx context.Context,
	opts upgradeOptions,
) (upgradeTargetVersion, error) {
	version := strings.TrimSpace(opts.Version)
	if version != "" {
		return upgradeTargetVersion{
			Version: version,
			Pinned:  true,
		}, nil
	}

	channel := strings.TrimSpace(opts.Channel)
	if channel == "" {
		channel = releaseinfo.ChannelLatest
	}
	latest, err := releaseClient().FetchChannelVersion(ctx, channel)
	if err != nil {
		return upgradeTargetVersion{}, fmt.Errorf(
			"get %s release version: %w",
			channel,
			err,
		)
	}
	return upgradeTargetVersion{
		Version: latest,
	}, nil
}

func lookupUpgradeSuggestion(
	ctx context.Context,
) (upgradeSuggestion, bool, error) {
	current := currentVersion()
	latest, err := releaseClient().FetchLatestVersion(ctx)
	if err != nil {
		return upgradeSuggestion{}, false, err
	}

	suggestion := upgradeSuggestion{
		CurrentVersion: current,
		LatestVersion:  latest,
	}
	if !releaseinfo.HasNewerRelease(latest, current) {
		return suggestion, false, nil
	}

	recentChanges, err := fetchLatestReleaseChanges(ctx, latest)
	if err == nil {
		suggestion.RecentChanges = recentChanges
	}
	return suggestion, true, nil
}

func fetchLatestReleaseChanges(
	ctx context.Context,
	version string,
) ([]string, error) {
	body, err := releaseClient().FetchChangelog(ctx, version)
	if err != nil {
		return nil, err
	}
	return releaseinfo.ExtractReleaseChanges(
		body,
		version,
		upgradeRecentChangesLimit,
	), nil
}

func buildUpgradeSuggestionText(
	suggestion upgradeSuggestion,
) string {
	var builder strings.Builder
	builder.WriteString("A newer trpc-claw release is available.\n")
	builder.WriteString("Current: ")
	builder.WriteString(strings.TrimSpace(suggestion.CurrentVersion))
	builder.WriteString("\nLatest:  ")
	builder.WriteString(strings.TrimSpace(suggestion.LatestVersion))
	builder.WriteString("\nUpgrade with:\n")
	builder.WriteString("  trpc-claw upgrade\n")
	builder.WriteString("  trpc-claw upgrade -f")
	if len(suggestion.RecentChanges) == 0 {
		return builder.String()
	}

	builder.WriteString("\nRecent changes:\n")
	for i, change := range suggestion.RecentChanges {
		if i > 0 {
			builder.WriteByte('\n')
		}
		builder.WriteString(changelogBulletPrefix)
		builder.WriteString(strings.TrimSpace(change))
	}
	return builder.String()
}

func installRelease(
	ctx context.Context,
	version string,
	binDir string,
	configDir string,
	opts upgradeOptions,
	stdout io.Writer,
	stderr io.Writer,
) error {
	scriptPath, err := downloadInstallScript(ctx, opts.Channel)
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(scriptPath) }()

	cmd, err := buildInstallReleaseCommand(
		ctx,
		scriptPath,
		version,
		binDir,
		configDir,
		opts,
		stdout,
		stderr,
	)
	if err != nil {
		return err
	}
	return cmd.Run()
}

func buildInstallReleaseCommand(
	ctx context.Context,
	scriptPath string,
	version string,
	binDir string,
	configDir string,
	opts upgradeOptions,
	stdout io.Writer,
	stderr io.Writer,
) (*exec.Cmd, error) {
	workDir, err := resolveUpgradeWorkDir()
	if err != nil {
		return nil, err
	}

	cmd := exec.CommandContext(
		ctx,
		upgradeShellName,
		scriptPath,
		"--version", version,
		"--bin-dir", binDir,
		"--config-dir", configDir,
	)
	if channel := strings.TrimSpace(opts.Channel); channel != "" {
		cmd.Args = append(cmd.Args, "--channel", channel)
	}
	if opts.Profile != "" {
		cmd.Args = append(
			cmd.Args,
			"--profile",
			opts.Profile,
		)
	}
	if opts.ForceConfig {
		cmd.Args = append(cmd.Args, "--force-config")
	}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.Dir = workDir
	return cmd, nil
}

func resolveUpgradeWorkDir() (string, error) {
	dir := strings.TrimSpace(tempDirFunc())
	if dir == "" {
		home, err := userHomeDirFunc()
		if err != nil {
			return "", fmt.Errorf(
				"resolve upgrade work dir: %w",
				err,
			)
		}
		dir = strings.TrimSpace(home)
	}
	if dir == "" {
		return "", fmt.Errorf("resolve upgrade work dir")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf(
			"prepare upgrade work dir %q: %w",
			dir,
			err,
		)
	}
	return dir, nil
}

func downloadInstallScript(
	ctx context.Context,
	channel string,
) (string, error) {
	body, err := fetchReleaseAsset(ctx, channelInstallURL(channel))
	if err != nil {
		return "", err
	}

	tmpFile, err := os.CreateTemp("", "trpc-claw-install-*.sh")
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}

	path := tmpFile.Name()
	if _, err := tmpFile.Write(body); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(path)
		return "", fmt.Errorf("write install script: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("close install script: %w", err)
	}
	if err := os.Chmod(path, 0o700); err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("chmod install script: %w", err)
	}
	return path, nil
}

func fetchReleaseAsset(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		url,
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request %q: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(
			"request %q failed: status %s",
			url,
			resp.Status,
		)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}
	return body, nil
}

func latestInstallURL() string {
	return channelInstallURL(releaseinfo.ChannelLatest)
}

func channelInstallURL(channel string) string {
	normalized, err := releaseinfo.NormalizeChannel(channel)
	if err != nil {
		normalized = releaseinfo.ChannelLatest
	}
	return joinReleaseURL(
		releaseBaseURL,
		strings.Join(
			[]string{
				normalized,
				channelInstallName,
			},
			"/",
		),
	)
}

func joinReleaseURL(base string, rel string) string {
	return releaseinfo.JoinURL(base, rel)
}

func currentBinaryDir() (string, error) {
	path, err := executablePathFunc()
	if err != nil {
		return "", fmt.Errorf("resolve executable path: %w", err)
	}
	dir := strings.TrimSpace(filepath.Dir(path))
	if dir == "" || dir == "." {
		return "", fmt.Errorf("resolve binary dir from %q", path)
	}
	return dir, nil
}

func resolveUpgradeConfigDir(paths startupPaths) (string, error) {
	if path := strings.TrimSpace(paths.OpenClawConfigPath); path != "" {
		return filepath.Dir(path), nil
	}
	if path := strings.TrimSpace(paths.TRPCConfigPath); path != "" {
		return filepath.Dir(path), nil
	}

	home, err := userHomeDirFunc()
	if err != nil {
		return "", fmt.Errorf("resolve user home: %w", err)
	}
	return filepath.Join(
		home,
		defaultConfigRootDir,
		defaultConfigAppDir,
	), nil
}

func releaseClient() releaseinfo.Client {
	return releaseinfo.Client{
		BaseURL: releaseBaseURL,
	}
}

func extractReleaseChanges(
	markdown string,
	version string,
	limit int,
) []string {
	return releaseinfo.ExtractReleaseChanges(
		markdown,
		version,
		limit,
	)
}

func compareReleaseVersions(left string, right string) int {
	return releaseinfo.CompareVersions(left, right)
}
