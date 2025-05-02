package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	cmap "github.com/orcaman/concurrent-map/v2"

	"github.com/ThinkInAIXYZ/go-mcp/pkg"
	"github.com/ThinkInAIXYZ/go-mcp/protocol"
	"github.com/ThinkInAIXYZ/go-mcp/transport"
)

var ErrQueueNotOpened = errors.New("queue has not been opened")

type State struct {
	ID string

	lastActiveAt time.Time

	transport transport.ServerTransport

	mu       sync.RWMutex
	sendChan chan []byte

	requestID int64

	reqID2respChan cmap.ConcurrentMap[string, chan *protocol.JSONRPCResponse]

	// cache client initialize request info
	clientInfo         *protocol.Implementation
	clientCapabilities *protocol.ClientCapabilities

	// subscribed resources
	subscribedResources cmap.ConcurrentMap[string, struct{}]

	receivedInitRequest *pkg.AtomicBool
	ready               *pkg.AtomicBool
	closed              *pkg.AtomicBool
}

func NewState(sessionID string) *State {
	return &State{
		ID:                  sessionID,
		lastActiveAt:        time.Now(),
		reqID2respChan:      cmap.New[chan *protocol.JSONRPCResponse](),
		subscribedResources: cmap.New[struct{}](),
		receivedInitRequest: pkg.NewAtomicBool(),
		ready:               pkg.NewAtomicBool(),
		closed:              pkg.NewAtomicBool(),
	}
}

func (s *State) SetClientInfo(ClientInfo *protocol.Implementation, ClientCapabilities *protocol.ClientCapabilities) {
	s.clientInfo = ClientInfo
	s.clientCapabilities = ClientCapabilities
}

func (s *State) GetClientCapabilities() *protocol.ClientCapabilities {
	return s.clientCapabilities
}

func (s *State) SetReceivedInitRequest(transport transport.ServerTransport) {
	s.transport = transport
	s.receivedInitRequest.Store(true)
}

func (s *State) GetReceivedInitRequest() bool {
	return s.receivedInitRequest.Load()
}

func (s *State) SetReady() {
	s.ready.Store(true)
}

func (s *State) GetReady() bool {
	return s.ready.Load()
}

func (s *State) GetReqID2respChan() cmap.ConcurrentMap[string, chan *protocol.JSONRPCResponse] {
	return s.reqID2respChan
}

func (s *State) GetSubscribedResources() cmap.ConcurrentMap[string, struct{}] {
	return s.subscribedResources
}

// CallClient Responsible for request and response assembly
func (s *State) CallClient(ctx context.Context, method protocol.Method, params protocol.ServerRequest) (json.RawMessage, error) {
	if s.closed.Load() {
		return nil, errors.New("session already closed")
	}

	requestID := strconv.FormatInt(s.incRequestID(), 10)
	respChan := make(chan *protocol.JSONRPCResponse, 1)
	s.GetReqID2respChan().Set(requestID, respChan)
	defer s.GetReqID2respChan().Remove(requestID)

	if err := s.sendMsgWithRequest(ctx, requestID, method, params); err != nil {
		return nil, fmt.Errorf("callClient: %w", err)
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case response := <-respChan:
		if err := response.Error; err != nil {
			return nil, pkg.NewResponseError(err.Code, err.Message, err.Data)
		}
		return response.RawResult, nil
	}
}

func (s *State) ReceiveNotify(notify *protocol.JSONRPCNotification) error {
	if notify.Method != protocol.NotificationInitialized && !s.GetReady() {
		return pkg.ErrSessionHasNotInitialized
	}

	switch notify.Method {
	case protocol.NotificationInitialized:
		return s.handleNotifyWithInitialized(notify.RawParams)
	default:
		return fmt.Errorf("%w: method=%s", pkg.ErrMethodNotSupport, notify.Method)
	}
}

func (s *State) ReceiveResponse(response *protocol.JSONRPCResponse) error {
	respChan, ok := s.GetReqID2respChan().Get(fmt.Sprint(response.ID))
	if !ok {
		return fmt.Errorf("%w: sessionID=%+v, requestID=%+v", pkg.ErrLackResponseChan, s.ID, response.ID)
	}

	select {
	case respChan <- response:
	default:
		return fmt.Errorf("%w: sessionID=%+v, response=%+v", pkg.ErrDuplicateResponseReceived, s.ID, response)
	}
	return nil
}

func (s *State) handleNotifyWithInitialized(rawParams json.RawMessage) error {
	param := &protocol.InitializedNotification{}
	if len(rawParams) > 0 {
		if err := pkg.JSONUnmarshal(rawParams, param); err != nil {
			return err
		}
	}

	if !s.GetReceivedInitRequest() {
		return fmt.Errorf("the server has not received the client's initialization request")
	}
	s.SetReady()
	return nil
}

func (s *State) SendMsgWithNotification(ctx context.Context, method protocol.Method, params protocol.ServerNotify) error {
	if s.closed.Load() {
		return errors.New("session already closed")
	}

	notify := protocol.NewJSONRPCNotification(method, params)

	message, err := json.Marshal(notify)
	if err != nil {
		return err
	}

	if err = s.transport.Send(ctx, s.ID, message); err != nil {
		return fmt.Errorf("sendNotification: transport send: %w", err)
	}
	return nil
}

func (s *State) SendMsgWithResponse(ctx context.Context, resp *protocol.JSONRPCResponse) error {
	message, err := json.Marshal(resp)
	if err != nil {
		return err
	}

	if err = s.transport.Send(ctx, s.ID, message); err != nil {
		return fmt.Errorf("sendResponse: transport send: %w", err)
	}
	return nil
}

func (s *State) sendMsgWithRequest(ctx context.Context, requestID protocol.RequestID, method protocol.Method, params protocol.ServerRequest) error { //nolint:lll
	if s.closed.Load() {
		return errors.New("session already closed")
	}

	if requestID == nil {
		return fmt.Errorf("requestID can't is nil")
	}

	req := protocol.NewJSONRPCRequest(requestID, method, params)

	message, err := json.Marshal(req)
	if err != nil {
		return err
	}

	if err = s.transport.Send(ctx, s.ID, message); err != nil {
		return fmt.Errorf("sendRequest: transport send: %w", err)
	}
	return nil
}

func (s *State) incRequestID() int64 {
	return atomic.AddInt64(&s.requestID, 1)
}

func (s *State) close() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.closed.Store(true)

	if s.sendChan != nil {
		close(s.sendChan)
	}
}

func (s *State) updateLastActiveAt() {
	s.lastActiveAt = time.Now()
}

func (s *State) openMessageQueueForSend() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.sendChan == nil {
		s.sendChan = make(chan []byte, 64)
	}
}

func (s *State) enqueueMessage(ctx context.Context, message []byte) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed.Load() {
		return errors.New("session already closed")
	}

	if s.sendChan == nil {
		return ErrQueueNotOpened
	}

	select {
	case s.sendChan <- message:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *State) dequeueMessage(ctx context.Context) ([]byte, error) {
	s.mu.RLock()
	if s.sendChan == nil {
		s.mu.RUnlock()
		return nil, ErrQueueNotOpened
	}
	s.mu.RUnlock()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case msg, ok := <-s.sendChan:
		if msg == nil && !ok {
			// There are no new messages and the chan has been closed, indicating that the request may need to be terminated.
			return nil, pkg.ErrSendEOF
		}
		return msg, nil
	}
}
