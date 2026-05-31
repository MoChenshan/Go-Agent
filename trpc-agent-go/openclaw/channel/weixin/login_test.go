package weixin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLoginWithQRSavesAccount(t *testing.T) {
	t.Parallel()

	stateDir := ResolveStateDir(t.TempDir(), "")
	statusCalls := 0
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(
		w http.ResponseWriter,
		r *http.Request,
	) {
		switch r.URL.Path {
		case "/" + endpointGetBotQRCode:
			writeJSONResponse(t, w, loginQRCodeResponse{
				QRCode:    "qr-1",
				QRCodeURL: "https://example.com/qr",
			})
		case "/" + endpointQRCodeStatus:
			statusCalls++
			if statusCalls == 1 {
				writeJSONResponse(t, w, loginQRCodeStatusResponse{
					Status: loginStatusScanned,
				})
				return
			}
			writeJSONResponse(t, w, loginQRCodeStatusResponse{
				Status:    loginStatusConfirmed,
				BotToken:  testToken,
				AccountID: testAccountID,
				BaseURL:   server.URL,
				UserID:    testPeerID,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(
		context.Background(),
		time.Second,
	)
	defer cancel()

	var qrURL string
	var statuses []string
	account, err := LoginWithQR(
		ctx,
		stateDir,
		server.URL,
		defaultLoginBotType,
		LoginCallbacks{
			OnQRCode: func(value string) {
				qrURL = value
			},
			OnStatus: func(status string) {
				statuses = append(statuses, status)
			},
		},
	)
	require.NoError(t, err)
	require.Equal(t, "https://example.com/qr", qrURL)
	require.Equal(t, testAccountID, account.AccountID)
	require.Equal(t, server.URL, account.BaseURL)
	require.Equal(t, testPeerID, account.UserID)
	require.Contains(t, statuses, loginStatusScanned)
	require.Contains(t, statuses, loginStatusConfirmed)

	accounts, err := ListAccounts(stateDir)
	require.NoError(t, err)
	require.Len(t, accounts, 1)
	require.Equal(t, testAccountID, accounts[0].AccountID)
}

func TestResolveRedirectBaseURL(t *testing.T) {
	t.Parallel()

	require.Equal(
		t,
		"https://redirect.example.com",
		resolveRedirectBaseURL(
			"https://ilinkai.weixin.qq.com",
			"https://redirect.example.com",
		),
	)
	require.Equal(
		t,
		"https://redirect.example.com/base",
		resolveRedirectBaseURL(
			"https://ilinkai.weixin.qq.com/base",
			"redirect.example.com",
		),
	)
}

func TestLoginWithQRRefreshesExpiredQRCode(t *testing.T) {
	t.Parallel()

	stateDir := ResolveStateDir(t.TempDir(), "")
	var qrCalls int
	var statusCalls int
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(
		w http.ResponseWriter,
		r *http.Request,
	) {
		switch r.URL.Path {
		case "/" + endpointGetBotQRCode:
			qrCalls++
			writeJSONResponse(t, w, loginQRCodeResponse{
				QRCode: "qr-" + string(rune('0'+qrCalls)),
				QRCodeURL: "https://example.com/qr/" +
					string(rune('0'+qrCalls)),
			})
		case "/" + endpointQRCodeStatus:
			statusCalls++
			switch statusCalls {
			case 1:
				writeJSONResponse(t, w, loginQRCodeStatusResponse{
					Status: loginStatusExpired,
				})
			default:
				writeJSONResponse(t, w, loginQRCodeStatusResponse{
					Status:    loginStatusConfirmed,
					BotToken:  testToken,
					AccountID: testAccountID,
					BaseURL:   server.URL,
					UserID:    testPeerID,
				})
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(
		context.Background(),
		time.Second,
	)
	defer cancel()

	var qrURLs []string
	account, err := LoginWithQR(
		ctx,
		stateDir,
		server.URL,
		defaultLoginBotType,
		LoginCallbacks{
			OnQRCode: func(value string) {
				qrURLs = append(qrURLs, value)
			},
		},
	)
	require.NoError(t, err)
	require.Equal(t, testAccountID, account.AccountID)
	require.Equal(
		t,
		[]string{
			"https://example.com/qr/1",
			"https://example.com/qr/2",
		},
		qrURLs,
	)
	require.Equal(t, 2, qrCalls)
}
