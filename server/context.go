package server

import (
	"context"
	"errors"

	"github.com/ThinkInAIXYZ/go-mcp/server/session"
)

type sessionKey struct{}

func setSessionToCtx(ctx context.Context, session *session.State) context.Context {
	return context.WithValue(ctx, sessionKey{}, session)
}

func getSessionFromCtx(ctx context.Context) (*session.State, error) {
	s := ctx.Value(sessionKey{})
	if s == nil {
		return nil, errors.New("no session found")
	}
	return s.(*session.State), nil
}
