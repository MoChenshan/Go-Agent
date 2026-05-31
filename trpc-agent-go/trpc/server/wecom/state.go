package wecom

import (
	"strconv"
	"sync"
	"time"
)

type inflightRequests struct {
	mu sync.Mutex
	m  map[string]string
}

func newInflightRequests() *inflightRequests {
	return &inflightRequests{m: make(map[string]string)}
}

func (r *inflightRequests) Get(sessionID string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.m[sessionID]
}

func (r *inflightRequests) Set(sessionID, requestID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.m[sessionID] = requestID
}

func (r *inflightRequests) Clear(sessionID, requestID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.m[sessionID] == requestID {
		delete(r.m, sessionID)
	}
}

type laneLocker struct {
	mu    sync.Mutex
	lanes map[string]*laneEntry
}

type laneEntry struct {
	lock sync.Mutex
	refs int
}

func newLaneLocker() *laneLocker {
	return &laneLocker{lanes: make(map[string]*laneEntry)}
}

func (l *laneLocker) withLockErrNotify(
	key string,
	onWait func(),
	fn func() error,
) error {
	if fn == nil {
		return nil
	}
	entry, waited := l.acquire(key)
	if waited && onWait != nil {
		onWait()
	}

	entry.lock.Lock()
	defer func() {
		entry.lock.Unlock()
		l.release(key, entry)
	}()
	return fn()
}

func (l *laneLocker) acquire(key string) (*laneEntry, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	entry, ok := l.lanes[key]
	if ok {
		entry.refs++
		return entry, true
	}
	entry = &laneEntry{refs: 1}
	l.lanes[key] = entry
	return entry, false
}

func (l *laneLocker) release(key string, entry *laneEntry) {
	l.mu.Lock()
	defer l.mu.Unlock()
	current, ok := l.lanes[key]
	if !ok || current != entry {
		return
	}
	entry.refs--
	if entry.refs <= 0 {
		delete(l.lanes, key)
	}
}

type sessionStore struct {
	mu      sync.Mutex
	current map[string]string
}

func newSessionStore() *sessionStore {
	return &sessionStore{current: make(map[string]string)}
}

func (s *sessionStore) Active(chatID, userID string) string {
	base := baseSessionID(chatID, userID)
	s.mu.Lock()
	defer s.mu.Unlock()
	sessionID := s.current[base]
	if sessionID == "" {
		sessionID = base
		s.current[base] = sessionID
	}
	return sessionID
}

func (s *sessionStore) Reset(chatID, userID string) string {
	base := baseSessionID(chatID, userID)
	sessionID := base + ":" + strconv.FormatInt(
		time.Now().UTC().UnixNano(),
		36,
	)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.current[base] = sessionID
	return sessionID
}
