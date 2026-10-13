package llm

import "context"

// MockProvider is a scripted Provider for tests across packages (QA-01).
type MockProvider struct {
	Name  string
	Reply string
	Err   error
	Calls int
}

func (m *MockProvider) ID() string { return m.Name }
func (m *MockProvider) Chat(_ context.Context, r ChatRequest) (ChatResponse, error) {
	m.Calls++
	return ChatResponse{Text: m.Reply, Usage: Usage{Provider: m.Name, Model: r.Model}}, m.Err
}
func (m *MockProvider) Complete(_ context.Context, model, _, _ string, _ int64) (string, Usage, error) {
	m.Calls++
	return m.Reply, Usage{Provider: m.Name, Model: model}, m.Err
}
func (m *MockProvider) Vision(_ context.Context, model string, _ []byte, _, _, _ string) (string, Usage, error) {
	m.Calls++
	return m.Reply, Usage{Provider: m.Name, Model: model, ImageCount: 1}, m.Err
}
