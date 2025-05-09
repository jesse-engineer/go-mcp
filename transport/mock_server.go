package transport

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/ThinkInAIXYZ/go-mcp/pkg"
)

type mockServerTransport struct {
	receiver serverReceiver
	in       io.ReadCloser
	out      io.Writer

	sessionID string

	sessionManager sessionManager

	logger pkg.Logger

	cancel          context.CancelFunc
	receiveShutDone chan struct{}
}

func NewMockServerTransport(in io.ReadCloser, out io.Writer) ServerTransport {
	return &mockServerTransport{
		in:     in,
		out:    out,
		logger: pkg.DefaultLogger,

		receiveShutDone: make(chan struct{}),
	}
}

func (t *mockServerTransport) Run() error {
	ctx, cancel := context.WithCancel(context.Background())
	t.cancel = cancel

	t.sessionID = t.sessionManager.CreateSession(false)

	t.startReceive(ctx)

	close(t.receiveShutDone)
	return nil
}

func (t *mockServerTransport) Send(_ context.Context, _ string, msg Message) error {
	if _, err := t.out.Write(append(msg, mcpMessageDelimiter)); err != nil {
		return fmt.Errorf("failed to write: %w", err)
	}
	return nil
}

func (t *mockServerTransport) SetReceiver(receiver serverReceiver) {
	t.receiver = receiver
}

func (t *mockServerTransport) SetSessionManager(m sessionManager) {
	t.sessionManager = m
}

func (t *mockServerTransport) Shutdown(userCtx context.Context, serverCtx context.Context) error {
	t.cancel()

	if err := t.in.Close(); err != nil {
		return err
	}

	<-t.receiveShutDone

	select {
	case <-serverCtx.Done():
		return nil
	case <-userCtx.Done():
		return userCtx.Err()
	}
}

func (t *mockServerTransport) startReceive(ctx context.Context) {
	s := bufio.NewReader(t.in)

	for {
		line, err := s.ReadBytes('\n')
		if err != nil {
			if errors.Is(err, io.ErrClosedPipe) || // This error occurs during unit tests, suppressing it here
				errors.Is(err, io.EOF) {
				return
			}
			t.logger.Errorf("client receive unexpected error reading input: %v", err)
			return
		}

		line = bytes.TrimRight(line, "\n")

		select {
		case <-ctx.Done():
			return
		default:
			if err = t.receiver.Receive(ctx, t.sessionID, line); err != nil {
				t.logger.Errorf("receiver failed: %v", err)
			}
		}
	}
}
