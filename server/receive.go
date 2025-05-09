package server

import (
	"context"
	"errors"
	"fmt"

	"github.com/tidwall/gjson"

	"github.com/ThinkInAIXYZ/go-mcp/pkg"
	"github.com/ThinkInAIXYZ/go-mcp/protocol"
)

func (server *Server) receive(ctx context.Context, sessionID string, msg []byte) error {
	if !gjson.GetBytes(msg, "id").Exists() {
		notify := &protocol.JSONRPCNotification{}
		if err := pkg.JSONUnmarshal(msg, &notify); err != nil {
			return err
		}
		s, err := server.sessionManager.GetSessionWithErr(sessionID)
		if err != nil {
			return err
		}

		if err = s.ReceiveNotify(notify); err != nil {
			notify.RawParams = nil // simplified log
			server.logger.Errorf("receive notify:%+v error: %s", notify, err.Error())
			return err
		}
		return nil
	}

	// case request or response
	if !gjson.GetBytes(msg, "method").Exists() {
		resp := &protocol.JSONRPCResponse{}
		if err := pkg.JSONUnmarshal(msg, &resp); err != nil {
			return err
		}

		s, err := server.sessionManager.GetSessionWithErr(sessionID)
		if err != nil {
			return err
		}

		if err = s.ReceiveResponse(resp); err != nil {
			resp.RawResult = nil // simplified log
			server.logger.Errorf("receive response:%+v error: %s", resp, err.Error())
			return err
		}
		return nil
	}

	req := &protocol.JSONRPCRequest{}
	if err := pkg.JSONUnmarshal(msg, &req); err != nil {
		return err
	}
	if !req.IsValid() {
		return pkg.ErrRequestInvalid
	}

	if sessionID != "" && req.Method != protocol.Initialize && req.Method != protocol.Ping {
		if s, ok := server.sessionManager.GetSession(sessionID); !ok {
			return pkg.ErrLackSession
		} else if !s.IsReady() {
			return pkg.ErrSessionHasNotInitialized
		}
	}

	server.inFlyRequest.Add(1)

	if server.inShutdown.Load() {
		server.inFlyRequest.Done()
		return errors.New("server already shutdown")
	}

	go func(ctx context.Context) {
		defer pkg.Recover()
		defer server.inFlyRequest.Done()
		defer server.sessionManager.CloseMessageQueueForSend(ctx, sessionID)

		if err := server.receiveRequest(ctx, sessionID, req); err != nil {
			req.RawParams = nil
			server.logger.Errorf("receive request:%+v error: %s", req, err.Error())
			return
		}
	}(pkg.NewCancelShieldContext(ctx))
	return nil
}

func (server *Server) receiveRequest(ctx context.Context, sessionID string, request *protocol.JSONRPCRequest) error {
	if s, ok := server.sessionManager.GetSession(sessionID); ok {
		ctx = setSessionToCtx(ctx, s)
	}

	if request.Method != protocol.Ping {
		server.sessionManager.UpdateSessionLastActiveAt(sessionID)
	}

	var (
		result protocol.ServerResponse
		err    error
	)

	switch request.Method {
	case protocol.Ping:
		result, err = server.handleRequestWithPing()
	case protocol.Initialize:
		result, err = server.handleRequestWithInitialize(ctx, sessionID, request.RawParams)
	case protocol.PromptsList:
		result, err = server.handleRequestWithListPrompts(request.RawParams)
	case protocol.PromptsGet:
		result, err = server.handleRequestWithGetPrompt(ctx, request.RawParams)
	case protocol.ResourcesList:
		result, err = server.handleRequestWithListResources(request.RawParams)
	case protocol.ResourceListTemplates:
		result, err = server.handleRequestWithListResourceTemplates(request.RawParams)
	case protocol.ResourcesRead:
		result, err = server.handleRequestWithReadResource(ctx, request.RawParams)
	case protocol.ResourcesSubscribe:
		result, err = server.handleRequestWithSubscribeResourceChange(sessionID, request.RawParams)
	case protocol.ResourcesUnsubscribe:
		result, err = server.handleRequestWithUnSubscribeResourceChange(sessionID, request.RawParams)
	case protocol.ToolsList:
		result, err = server.handleRequestWithListTools(request.RawParams)
	case protocol.ToolsCall:
		result, err = server.handleRequestWithCallTool(ctx, request.RawParams)
	default:
		err = fmt.Errorf("%w: method=%s", pkg.ErrMethodNotSupport, request.Method)
	}

	var resp *protocol.JSONRPCResponse
	if err != nil {
		var code int
		switch {
		case errors.Is(err, pkg.ErrMethodNotSupport):
			code = protocol.MethodNotFound
		case errors.Is(err, pkg.ErrRequestInvalid):
			code = protocol.InvalidRequest
		case errors.Is(err, pkg.ErrJSONUnmarshal):
			code = protocol.ParseError
		default:
			code = protocol.InternalError
		}
		resp = protocol.NewJSONRPCErrorResponse(request.ID, code, err.Error())
	} else {
		resp = protocol.NewJSONRPCSuccessResponse(request.ID, result)
	}

	s, err := server.sessionManager.GetSessionWithErr(sessionID)
	if err != nil {
		return err
	}
	if err = s.SendMsgWithResponse(ctx, resp); err != nil {
		return err
	}
	return nil
}
