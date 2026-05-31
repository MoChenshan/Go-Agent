package weixin

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	defaultLoginTimeout = 8 * time.Minute
	maxLoginQRRefreshes = 3
)

type LoginCallbacks struct {
	OnQRCode func(qrURL string)
	OnStatus func(status string)
}

func LoginWithQR(
	ctx context.Context,
	stateRoot string,
	apiBaseURL string,
	botType string,
	callbacks LoginCallbacks,
) (Account, error) {
	apiBaseURL = defaultString(apiBaseURL, defaultBaseURL)
	client := newAPIClient(&http.Client{
		Timeout: defaultPollTimeout + defaultPollTimeoutGrace,
	})

	startCtx, cancel := context.WithTimeout(
		ctx,
		defaultAPIRequestTimeout,
	)
	defer cancel()

	qr, err := client.startLoginQR(startCtx, apiBaseURL, botType)
	if err != nil {
		return Account{}, err
	}
	if callbacks.OnQRCode != nil {
		callbacks.OnQRCode(strings.TrimSpace(qr.QRCodeURL))
	}

	lastStatus := ""
	pollBaseURL := apiBaseURL
	qrRefreshes := 1
	for {
		if ctx.Err() != nil {
			return Account{}, ctx.Err()
		}

		pollCtx, pollCancel := context.WithTimeout(
			ctx,
			defaultPollTimeout+defaultPollTimeoutGrace,
		)
		status, err := client.pollLoginQR(
			pollCtx,
			pollBaseURL,
			qr.QRCode,
		)
		pollCancel()
		if err != nil {
			return Account{}, err
		}
		if status.Status != lastStatus &&
			callbacks.OnStatus != nil {
			callbacks.OnStatus(status.Status)
			lastStatus = status.Status
		}

		switch status.Status {
		case "", loginStatusWait, loginStatusScanned:
			continue
		case loginStatusScannedRedirected:
			pollBaseURL = resolveRedirectBaseURL(
				pollBaseURL,
				status.RedirectHost,
			)
			continue
		case loginStatusExpired:
			qrRefreshes++
			if qrRefreshes > maxLoginQRRefreshes {
				return Account{}, fmt.Errorf(
					"weixin login: qr code expired",
				)
			}
			refreshCtx, refreshCancel := context.WithTimeout(
				ctx,
				defaultAPIRequestTimeout,
			)
			qr, err = client.startLoginQR(
				refreshCtx,
				apiBaseURL,
				botType,
			)
			refreshCancel()
			if err != nil {
				return Account{}, err
			}
			pollBaseURL = apiBaseURL
			lastStatus = ""
			if callbacks.OnQRCode != nil {
				callbacks.OnQRCode(
					strings.TrimSpace(qr.QRCodeURL),
				)
			}
			continue
		case loginStatusConfirmed:
			account := Account{
				AccountID: strings.TrimSpace(status.AccountID),
				Token:     strings.TrimSpace(status.BotToken),
				BaseURL: defaultString(
					status.BaseURL,
					pollBaseURL,
				),
				UserID: strings.TrimSpace(status.UserID),
			}
			if account.AccountID == "" || account.Token == "" {
				return Account{}, fmt.Errorf(
					"weixin login: incomplete login result",
				)
			}
			if err := SaveAccount(stateRoot, account); err != nil {
				return Account{}, err
			}
			return account, nil
		default:
			return Account{}, fmt.Errorf(
				"weixin login: unsupported status %q",
				status.Status,
			)
		}
	}
}

func resolveRedirectBaseURL(
	currentBaseURL string,
	redirectHost string,
) string {
	redirectHost = strings.TrimSpace(redirectHost)
	if redirectHost == "" {
		return currentBaseURL
	}
	if strings.HasPrefix(redirectHost, "http://") ||
		strings.HasPrefix(redirectHost, "https://") {
		return redirectHost
	}

	parsed, err := url.Parse(currentBaseURL)
	if err != nil {
		return currentBaseURL
	}
	parsed.Host = redirectHost
	return parsed.String()
}
