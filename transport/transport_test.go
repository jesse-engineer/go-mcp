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
	session2streamID2ch    pkg.SyncMap[*pkg.SyncMap[chan []byte]]
	session2closedStreamID pkg.SyncMap[*pkg.SyncMap[struct{}]]
}

func newMockSessionManager() *mockSessionManager {
	return &mockSessionManager{}
}

func (m *mockSessionManager) CreateSession(_ bool) string {
	sessionID := uuid.NewString()
	m.session2streamID2ch.Store(sessionID, &pkg.SyncMap[chan []byte]{})
	m.session2closedStreamID.Store(sessionID, &pkg.SyncMap[struct{}]{})
	return sessionID
}

func (m *mockSessionManager) OpenMessageQueueForSend(ctx context.Context, sessionID string) error {
	streamID2ch, ok := m.session2streamID2ch.Load(sessionID)
	if !ok {
		return pkg.ErrLackSession
	}
	streamID, _ := GetStreamIDFromCtx(ctx)
	streamID2ch.Store(streamID, make(chan []byte, 1))
	return nil
}

func (m *mockSessionManager) EnqueueMessageForSend(ctx context.Context, sessionID string, message []byte) error {
	streamID2ch, has := m.session2streamID2ch.Load(sessionID)
	if !has {
		return pkg.ErrLackSession
	}

	streamID, _ := GetStreamIDFromCtx(ctx)
	ch, ok := streamID2ch.Load(streamID)
	if !ok {
		return pkg.ErrQueueNotOpened
	}

	select {
	case ch <- message:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *mockSessionManager) DequeueMessageForSend(ctx context.Context, sessionID string) ([]byte, error) {
	streamID2ch, has := m.session2streamID2ch.Load(sessionID)
	if !has {
		return nil, pkg.ErrLackSession
	}

	streamID, _ := GetStreamIDFromCtx(ctx)
	ch, ok := streamID2ch.Load(streamID)
	if !ok {
		return nil, pkg.ErrQueueNotOpened
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case msg, ok := <-ch:
		if msg == nil && !ok {
			// There are no new messages and the chan has been closed, indicating that the request may need to be terminated.
			return nil, pkg.ErrDequeueMessageEOF
		}
		closedStreamID, _ := m.session2closedStreamID.Load(sessionID)
		closedStreamID.Store(streamID, struct{}{})
		close(ch)
		return msg, nil
	}
}

func (m *mockSessionManager) CloseSession(sessionID string) {
	streamID2ch, _ := m.session2streamID2ch.LoadAndDelete(sessionID)
	closeStreamIDSet, _ := m.session2closedStreamID.Load(sessionID)
	defer m.session2closedStreamID.Delete(sessionID)

	streamID2ch.Range(func(streamID string, ch chan []byte) bool {
		if _, ok := closeStreamIDSet.Load(streamID); ok {
			return true
		}
		close(ch)
		return true
	})
}

func (m *mockSessionManager) CloseAllSessions() {
	m.session2streamID2ch.Range(func(sessionID string, _ *pkg.SyncMap[chan []byte]) bool {
		m.CloseSession(sessionID)
		return true
	})
}

func testTransport(t *testing.T, client ClientTransport, server ServerTransport) {
	testMsg := "hello server"
	expectedMsgWithServerCh := make(chan string, 3)
	server.SetSessionManager(newMockSessionManager())
	server.SetReceiver(ServerReceiverF(func(ctx context.Context, sessionID string, msg []byte) error {
		expectedMsgWithServerCh <- string(msg)
		if srv, ok := server.(*streamableHTTPServerTransport); ok {
			return srv.sessionManager.EnqueueMessageForSend(ctx, sessionID, msg)
		}
		return nil
	}))

	expectedMsgWithClientCh := make(chan string, 3)
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

	if srv, ok := server.(*streamableHTTPServerTransport); ok && srv.stateMode == Stateless {
		return
	}

	sessionID := ""
	if cli, ok := client.(*sseClientTransport); ok {
		sessionID = cli.messageEndpoint.Query().Get("sessionID")
	}

	if err := server.Send(context.Background(), sessionID, Message(testMsg)); err != nil {
		t.Fatalf("server.Send() failed: %v", err)
	}
	expectedMsg = <-expectedMsgWithClientCh
	if !reflect.DeepEqual(expectedMsg, testMsg) {
		t.Fatalf("server.Send() failed: got %v, want %v", expectedMsg, testMsg)
	}
}
