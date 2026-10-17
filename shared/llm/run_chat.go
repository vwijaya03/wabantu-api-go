package llm

import "context"

// RunChat runs a plain chat completion through the tenant's chain for fn (tenant row → global
// row → code default), advancing on retryable errors or link timeouts like RunToolChat.
func RunChat(ctx context.Context, reg *Registry, tenantID string, fn Function, req ChatRequest) (ChatResponse, RunResult, error) {
	var out ChatResponse
	res, err := reg.Run(ctx, tenantID, fn, func(ctx context.Context, p Provider, model string) error {
		r := req
		r.Model = model
		var e error
		out, e = p.Chat(ctx, r)
		return e
	})
	return out, res, err
}
