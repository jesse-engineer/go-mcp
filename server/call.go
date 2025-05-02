package server

import (
	"context"
	"fmt"

	"github.com/ThinkInAIXYZ/go-mcp/pkg"
	"github.com/ThinkInAIXYZ/go-mcp/protocol"
	"github.com/ThinkInAIXYZ/go-mcp/server/session"
)

func Ping(ctx context.Context, request *protocol.PingRequest) (*protocol.PingResult, error) {
	session, err := getSessionFromCtx(ctx)
	if err != nil {
		return nil, err
	}

	response, err := session.CallClient(ctx, protocol.Ping, request)
	if err != nil {
		return nil, err
	}

	var result protocol.PingResult
	if err = pkg.JSONUnmarshal(response, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}
	return &result, nil
}

func Sampling(ctx context.Context, request *protocol.CreateMessageRequest) (*protocol.CreateMessageResult, error) {
	session, err := getSessionFromCtx(ctx)
	if err != nil {
		return nil, err
	}

	if session.GetClientCapabilities() == nil || session.GetClientCapabilities().Sampling == nil {
		return nil, pkg.ErrServerNotSupport
	}

	response, err := session.CallClient(ctx, protocol.SamplingCreateMessage, request)
	if err != nil {
		return nil, err
	}

	var result protocol.CreateMessageResult
	if err = pkg.JSONUnmarshal(response, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}
	return &result, nil
}

func (server *Server) sendNotification4ToolListChanges(ctx context.Context) error {
	if server.capabilities.Tools == nil || !server.capabilities.Tools.ListChanged {
		return pkg.ErrServerNotSupport
	}

	var errList []error
	server.sessionManager.RangeSessions(func(sessionID string, session *session.State) bool {
		if err := session.SendMsgWithNotification(ctx, protocol.NotificationToolsListChanged, protocol.NewToolListChangedNotification()); err != nil {
			errList = append(errList, fmt.Errorf("sessionID=%s, err: %w", sessionID, err))
		}
		return true
	})
	return pkg.JoinErrors(errList)
}

func (server *Server) sendNotification4PromptListChanges(ctx context.Context) error {
	if server.capabilities.Prompts == nil || !server.capabilities.Prompts.ListChanged {
		return pkg.ErrServerNotSupport
	}

	var errList []error
	server.sessionManager.RangeSessions(func(sessionID string, session *session.State) bool {
		if err := session.SendMsgWithNotification(ctx, protocol.NotificationPromptsListChanged, protocol.NewPromptListChangedNotification()); err != nil {
			errList = append(errList, fmt.Errorf("sessionID=%s, err: %w", sessionID, err))
		}
		return true
	})
	return pkg.JoinErrors(errList)
}

func (server *Server) sendNotification4ResourceListChanges(ctx context.Context) error {
	if server.capabilities.Resources == nil || !server.capabilities.Resources.ListChanged {
		return pkg.ErrServerNotSupport
	}

	var errList []error
	server.sessionManager.RangeSessions(func(sessionID string, session *session.State) bool {
		if err := session.SendMsgWithNotification(ctx, protocol.NotificationResourcesListChanged,
			protocol.NewResourceListChangedNotification()); err != nil {
			errList = append(errList, fmt.Errorf("sessionID=%s, err: %w", sessionID, err))
		}
		return true
	})
	return pkg.JoinErrors(errList)
}

func (server *Server) SendNotification4ResourcesUpdated(ctx context.Context, notify *protocol.ResourceUpdatedNotification) error {
	if server.capabilities.Resources == nil || !server.capabilities.Resources.Subscribe {
		return pkg.ErrServerNotSupport
	}

	var errList []error
	server.sessionManager.RangeSessions(func(sessionID string, session *session.State) bool {
		if _, ok := session.GetSubscribedResources().Get(notify.URI); !ok {
			return true
		}

		if err := session.SendMsgWithNotification(ctx, protocol.NotificationResourcesUpdated, notify); err != nil {
			errList = append(errList, fmt.Errorf("sessionID=%s, err: %w", sessionID, err))
		}
		return true
	})
	return pkg.JoinErrors(errList)
}
