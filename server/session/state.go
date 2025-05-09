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

type State struct {
	id string

	disposable bool

	lastActiveAt time.Time

	transport transport.ServerTransport

	mu                sync.RWMutex
	streamID2sendChan map[string]chan []byte
	closedStreamIDSet map[string]struct{}

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

func NewState(sessionID string, disposable bool) *State {
	return &State{
		id:                  sessionID,
		disposable:          disposable,
		lastActiveAt:        time.Now(),
		streamID2sendChan:   make(map[string]chan []byte),
		closedStreamIDSet:   make(map[string]struct{}),
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
	if s.disposable {
		return true
	}
	return s.receivedInitRequest.Load()
}

func (s *State) SetReady() {
	s.ready.Store(true)
}

func (s *State) IsReady() bool {
	if s.disposable {
		return true
	}
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
	if notify.Method != protocol.NotificationInitialized && !s.IsReady() {
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
		return fmt.Errorf("%w: sessionID=%+v, requestID=%+v", pkg.ErrLackResponseChan, s.id, response.ID)
	}

	select {
	case respChan <- response:
	default:
		return fmt.Errorf("%w: sessionID=%+v, response=%+v", pkg.ErrDuplicateResponseReceived, s.id, response)
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

	if err = s.transport.Send(ctx, s.id, message); err != nil {
		return fmt.Errorf("sendNotification: transport send: %w", err)
	}
	return nil
}

func (s *State) SendMsgWithResponse(ctx context.Context, resp *protocol.JSONRPCResponse) error {
	message, err := json.Marshal(resp)
	if err != nil {
		return err
	}

	if err = s.transport.Send(ctx, s.id, message); err != nil {
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

	if err = s.transport.Send(ctx, s.id, message); err != nil {
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

	for streamID, ch := range s.streamID2sendChan {
		if _, ok := s.closedStreamIDSet[streamID]; ok {
			continue
		}
		close(ch)
	}
	s.streamID2sendChan = make(map[string]chan []byte)
}

func (s *State) updateLastActiveAt() {
	s.lastActiveAt = time.Now()
}

func (s *State) openMessageQueueForSend(streamID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.streamID2sendChan[streamID]; !ok {
		s.streamID2sendChan[streamID] = make(chan []byte, 1)
	}
}

func (s *State) closeMessageQueueForSend(streamID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if ch, ok := s.streamID2sendChan[streamID]; ok {
		if _, ok := s.closedStreamIDSet[streamID]; ok {
			return
		}
		s.closedStreamIDSet[streamID] = struct{}{}
		close(ch)
	}
}

func (s *State) enqueueMessage(ctx context.Context, streamID string, message []byte) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed.Load() {
		return errors.New("session already closed")
	}

	sendChan, ok := s.streamID2sendChan[streamID]
	if !ok {
		return pkg.ErrQueueNotOpened
	}

	select {
	case sendChan <- message:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *State) dequeueMessage(ctx context.Context, streamID string) ([]byte, error) {
	s.mu.RLock()
	sendChan, ok := s.streamID2sendChan[streamID]
	if !ok {
		s.mu.RUnlock()
		return nil, pkg.ErrQueueNotOpened
	}
	s.mu.RUnlock()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case msg, ok := <-sendChan:
		if msg == nil && !ok {
			// There are no new messages and the chan has been closed, indicating that the request may need to be terminated.
			return nil, pkg.ErrDequeueMessageEOF
		}
		return msg, nil
	}
}
