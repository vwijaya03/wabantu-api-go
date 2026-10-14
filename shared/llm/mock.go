package llm

import (
	"context"
	"sync"
)

// MockProvider is a scripted Provider for tests across packages (QA-01). Safe for concurrent use.
type MockProvider struct {
	Name  string
	Reply string
	Err   error
	Calls int
	// Unavailable makes Available() false (simulates missing key).
	Unavailable bool
	// Errs, if set, is consumed per call before Err.
	Errs []error

	mu sync.Mutex
}

func (m *MockProvider) ID() string { return m.Name }

func (m *MockProvider) Available() bool { return !m.Unavailable }

// next records a call and returns the scripted error; a done ctx wins (mock respects ctx).
func (m *MockProvider) next(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls++
	if ctx != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	if len(m.Errs) > 0 {
		e := m.Errs[0]
		m.Errs = m.Errs[1:]
		return e
	}
	return m.Err
}

// CallCount returns Calls under the lock.
func (m *MockProvider) CallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.Calls
}

func (m *MockProvider) Chat(ctx context.Context, r ChatRequest) (ChatResponse, error) {
	err := m.next(ctx)
	return ChatResponse{Text: m.Reply, Usage: Usage{Provider: m.Name, Model: r.Model}}, err
}

func (m *MockProvider) Complete(ctx context.Context, model, _, _ string, _ int64) (string, Usage, error) {
	err := m.next(ctx)
	return m.Reply, Usage{Provider: m.Name, Model: model}, err
}

func (m *MockProvider) Vision(ctx context.Context, model string, _ []byte, _, _, _ string) (string, Usage, error) {
	err := m.next(ctx)
	return m.Reply, Usage{Provider: m.Name, Model: model, ImageCount: 1}, err
}

// ToolScript, if set, makes MockProvider a ToolCaller: it is called once per ChatWithTools
// and may invoke exec to simulate tool rounds.
type MockToolScript func(req ToolChatRequest, exec ToolExecutor) (ToolChatResponse, error)

// MockToolProvider is a MockProvider that also implements ToolCaller.
type MockToolProvider struct {
	*MockProvider
	Script  MockToolScript
	LastReq ToolChatRequest
}

func (m *MockToolProvider) ChatWithTools(ctx context.Context, r ToolChatRequest, exec ToolExecutor) (ToolChatResponse, error) {
	if err := m.next(ctx); err != nil {
		return ToolChatResponse{Usage: Usage{Provider: m.Name, Model: r.Model}}, err
	}
	m.mu.Lock()
	m.LastReq = r
	m.mu.Unlock()
	if m.Script == nil {
		return ToolChatResponse{Text: m.Reply, Usage: Usage{Provider: m.Name, Model: r.Model}}, nil
	}
	out, err := m.Script(r, exec)
	out.Usage.Provider, out.Usage.Model = m.Name, r.Model
	return out, err
}
