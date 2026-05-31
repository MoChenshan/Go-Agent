package debug

import (
	"net/http"
	"strings"

	"git.woa.com/trpc-go/trpc-agent-go/trpc/server/debug/internal/schema"
	"github.com/gorilla/mux"
	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/session"
)

func (s *Server) handleListApps(w http.ResponseWriter, r *http.Request) {
	log.InfofContext(
		r.Context(),
		"handleListApps called: path=%s",
		r.URL.Path,
	)
	var apps []string
	for name := range s.agents {
		apps = append(apps, name)
	}
	s.writeJSON(w, apps)
}

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log.InfofContext(
		ctx,
		"handleListSessions called: path=%s",
		r.URL.Path,
	)
	vars := mux.Vars(r)
	appName := vars["appName"]
	userID := vars["userId"]

	userKey := session.UserKey{AppName: appName, UserID: userID}
	sessions, err := s.sessionSvc.ListSessions(ctx, userKey)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Convert internal sessions to ADK format.
	adkSessions := make([]schema.ADKSession, 0, len(sessions))
	for _, sess := range sessions {
		// Filter out eval sessions, same as Python ADK.
		if !strings.HasPrefix(sess.ID, "eval-") {
			adkSessions = append(adkSessions, convertSessionToADKFormat(sess))
		}
	}
	s.writeJSON(w, adkSessions)
}

func (s *Server) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log.InfofContext(
		ctx,
		"handleCreateSession called: path=%s",
		r.URL.Path,
	)
	vars := mux.Vars(r)
	appName := vars["appName"]
	userID := vars["userId"]

	key := session.Key{AppName: appName, UserID: userID}
	sess, err := s.sessionSvc.CreateSession(ctx, key, session.StateMap{})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.writeJSON(w, convertSessionToADKFormat(sess))
}

func (s *Server) handleGetSession(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log.InfofContext(
		ctx,
		"handleGetSession called: path=%s",
		r.URL.Path,
	)
	vars := mux.Vars(r)
	appName := vars["appName"]
	userID := vars["userId"]
	sessionID := vars["sessionId"]
	sess, err := s.sessionSvc.GetSession(ctx, session.Key{
		AppName:   appName,
		UserID:    userID,
		SessionID: sessionID,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if sess == nil {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}
	s.writeJSON(w, convertSessionToADKFormat(sess))
}
