package session

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/ThinkInAIXYZ/go-mcp/pkg"
	"github.com/ThinkInAIXYZ/go-mcp/transport"
)

type Manager struct {
	activeSessions pkg.SyncMap[*State]
	closedSessions pkg.SyncMap[struct{}]

	stopHeartbeat chan struct{}

	logger pkg.Logger

	detection   func(ctx context.Context, session *State) error
	maxIdleTime time.Duration
}

func NewManager(detection func(ctx context.Context, session *State) error) *Manager {
	return &Manager{
		detection:     detection,
		stopHeartbeat: make(chan struct{}),
		logger:        pkg.DefaultLogger,
	}
}

func (m *Manager) SetMaxIdleTime(d time.Duration) {
	m.maxIdleTime = d
}

func (m *Manager) SetLogger(logger pkg.Logger) {
	m.logger = logger
}

func (m *Manager) CreateSession(disposable bool) string {
	sessionID := uuid.NewString()
	state := NewState(sessionID, disposable)
	m.activeSessions.Store(sessionID, state)
	return sessionID
}

func (m *Manager) GetSession(sessionID string) (*State, bool) {
	s, err := m.GetSessionWithErr(sessionID)
	if err != nil {
		return nil, false
	}
	return s, true
}

func (m *Manager) GetSessionWithErr(sessionID string) (*State, error) {
	if sessionID == "" {
		return nil, errors.New("sessionID can't is nil")
	}

	s, has := m.activeSessions.Load(sessionID)
	if !has {
		if _, has = m.closedSessions.Load(sessionID); has {
			return nil, pkg.ErrSessionClosed
		}
		return nil, pkg.ErrLackSession
	}
	return s, nil
}

func (m *Manager) OpenMessageQueueForSend(ctx context.Context, sessionID string) error {
	state, has := m.GetSession(sessionID)
	if !has {
		return pkg.ErrLackSession
	}
	streamID, _ := transport.GetStreamIDFromCtx(ctx)
	state.openMessageQueueForSend(streamID)
	return nil
}

func (m *Manager) CloseMessageQueueForSend(ctx context.Context, sessionID string) {
	state, has := m.GetSession(sessionID)
	if !has {
		return
	}
	streamID, err := transport.GetStreamIDFromCtx(ctx)
	if err != nil {
		return
	}
	state.closeMessageQueueForSend(streamID)
}

func (m *Manager) EnqueueMessageForSend(ctx context.Context, sessionID string, message []byte) error {
	state, has := m.GetSession(sessionID)
	if !has {
		return pkg.ErrLackSession
	}
	streamID, _ := transport.GetStreamIDFromCtx(ctx)
	return state.enqueueMessage(ctx, streamID, message)
}

func (m *Manager) DequeueMessageForSend(ctx context.Context, sessionID string) ([]byte, error) {
	state, has := m.GetSession(sessionID)
	if !has {
		return nil, pkg.ErrLackSession
	}
	streamID, _ := transport.GetStreamIDFromCtx(ctx)
	return state.dequeueMessage(ctx, streamID)
}

func (m *Manager) UpdateSessionLastActiveAt(sessionID string) {
	state, ok := m.activeSessions.Load(sessionID)
	if !ok {
		return
	}
	state.updateLastActiveAt()
}

func (m *Manager) CloseSession(sessionID string) {
	state, ok := m.activeSessions.LoadAndDelete(sessionID)
	if !ok {
		return
	}
	state.close()
	m.closedSessions.Store(sessionID, struct{}{})
}

func (m *Manager) CloseAllSessions() {
	m.activeSessions.Range(func(sessionID string, _ *State) bool {
		// Here we load the session again to prevent concurrency conflicts with CloseSession, which may cause repeated close chan
		m.CloseSession(sessionID)
		return true
	})
}

func (m *Manager) StartHeartbeatAndCleanInvalidSessions() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopHeartbeat:
			return
		case <-ticker.C:
			now := time.Now()
			m.activeSessions.Range(func(sessionID string, state *State) bool {
				if state.disposable {
					return true
				}

				if m.maxIdleTime != 0 && now.Sub(state.lastActiveAt) > m.maxIdleTime {
					m.logger.Infof("session expire, session id: %v", sessionID)
					m.CloseSession(sessionID)
					return true
				}

				var err error
				for i := 0; i < 3; i++ {
					if err = m.detection(context.Background(), state); err == nil {
						return true
					}
				}
				m.logger.Infof("session detection fail, session id: %v, fail reason: %+v", sessionID, err)
				m.CloseSession(sessionID)
				return true
			})
		}
	}
}

func (m *Manager) StopHeartbeat() {
	close(m.stopHeartbeat)
}

func (m *Manager) RangeSessions(f func(sessionID string, state *State) bool) {
	m.activeSessions.Range(f)
}

func (m *Manager) IsEmpty() bool {
	isEmpty := true
	m.activeSessions.Range(func(string, *State) bool {
		isEmpty = false
		return false
	})
	return isEmpty
}
