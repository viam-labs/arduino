package arduino

import (
	"context"
	"fmt"
)

// mockSender implements sender for testing.
// responses is a queue of (response, error) pairs returned in order.
type mockSender struct {
	responses []mockResponse
	sent      []string
	closed    bool
}

type mockResponse struct {
	resp string
	err  error
}

func (m *mockSender) send(_ context.Context, cmd string) (string, error) {
	m.sent = append(m.sent, cmd)
	if len(m.responses) == 0 {
		return "", fmt.Errorf("mockSender: no more responses queued")
	}
	r := m.responses[0]
	m.responses = m.responses[1:]
	return r.resp, r.err
}

func (m *mockSender) close() error {
	m.closed = true
	return nil
}

func (m *mockSender) queue(resp string, err error) {
	m.responses = append(m.responses, mockResponse{resp, err})
}
