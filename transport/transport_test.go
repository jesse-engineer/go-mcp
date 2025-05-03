package transport

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/ThinkInAIXYZ/go-mcp/pkg"
)

type mockSessionManager struct {
	pkg.SyncMap[chan []byte]
}

func newMockSessionManager() *mockSessionManager {
	return &mockSessionManager{}
}

func (m *mockSessionManager) CreateSession() string {
	sessionID := uuid.NewString()
	m.Store(sessionID, nil)
	return sessionID
}

func (m *mockSessionManager) OpenMessageQueueForSend(sessionID string, streamID string) error {
	_, ok := m.Load(sessionID)
	if !ok {
		return pkg.ErrLackSession
	}
	m.Store(sessionID, make(chan []byte))
	return nil
}

func (m *mockSessionManager) IsExistSession(sessionID string) bool {
	_, has := m.Load(sessionID)
	return has
}

func (m *mockSessionManager) EnqueueMessageForSend(ctx context.Context, sessionID string, streamID string, message []byte) error {
	ch, has := m.Load(sessionID)
	if !has {
		return pkg.ErrLackSession
	}

	select {
	case ch <- message:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *mockSessionManager) DequeueMessageForSend(ctx context.Context, sessionID string, id string) ([]byte, error) {
	ch, has := m.Load(sessionID)
	if !has {
		return nil, pkg.ErrLackSession
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case msg, ok := <-ch:
		if msg == nil && !ok {
			// There are no new messages and the chan has been closed, indicating that the request may need to be terminated.
			return nil, pkg.ErrSendEOF
		}
		return msg, nil
	}
}

func (m *mockSessionManager) CloseSession(sessionID string) {
	ch, ok := m.LoadAndDelete(sessionID)
	if !ok {
		return
	}
	close(ch)
}

func (m *mockSessionManager) CloseAllSessions() {
	m.Range(func(key string, value chan []byte) bool {
		m.Delete(key)
		close(value)
		return true
	})
}

func testTransport(t *testing.T, client ClientTransport, server ServerTransport) {
	testMsg := "hello server"
	expectedMsgWithServerCh := make(chan string, 1)
	server.SetReceiver(ServerReceiverF(func(_ context.Context, _ string, msg []byte) error {
		expectedMsgWithServerCh <- string(msg)
		return nil
	}))
	server.SetSessionManager(newMockSessionManager())

	expectedMsgWithClientCh := make(chan string, 1)
	client.SetReceiver(ClientReceiverF(func(_ context.Context, msg []byte) error {
		expectedMsgWithClientCh <- string(msg)
		return nil
	}))

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Run()
	}()

	// Use select to handle potential errors
	select {
	case err := <-errCh:
		t.Fatalf("server.Run() failed: %v", err)
	case <-time.After(time.Second):
		// Server started normally
	}

	defer func() {
		if _, ok := server.(*stdioServerTransport); ok { // stdioServerTransport not support shutdown
			return
		}

		userCtx, cancel := context.WithTimeout(context.Background(), time.Second*1)
		defer cancel()

		serverCtx, cancel := context.WithCancel(userCtx)
		cancel()

		if err := server.Shutdown(userCtx, serverCtx); err != nil {
			t.Fatalf("server.Shutdown() failed: %v", err)
		}
	}()

	if err := client.Start(); err != nil {
		t.Fatalf("client.Run() failed: %v", err)
	}

	defer func() {
		if err := client.Close(); err != nil {
			t.Fatalf("client.Close() failed: %v", err)
		}
	}()

	if err := client.Send(context.Background(), Message(testMsg)); err != nil {
		t.Fatalf("client.Send() failed: %v", err)
	}
	expectedMsg := <-expectedMsgWithServerCh
	if !reflect.DeepEqual(expectedMsg, testMsg) {
		t.Fatalf("client.Send() got %v, want %v", expectedMsg, testMsg)
	}
	expectedMsg = <-expectedMsgWithClientCh
	if !reflect.DeepEqual(expectedMsg, testMsg) {
		t.Fatalf("server.Send() failed: got %v, want %v", expectedMsg, testMsg)
	}
}
