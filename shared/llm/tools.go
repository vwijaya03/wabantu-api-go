package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ToolDef is a provider-neutral tool declaration. Parameters is a JSON-Schema object
// ({"type":"object","properties":{...},"required":[...]}), as used by Anthropic input_schema.
type ToolDef struct {
	Name        string
	Description string
	Parameters  map[string]any
}

// ToolExecutor runs one tool call and returns its result (JSON text preferred).
type ToolExecutor func(name string, args json.RawMessage) (string, error)

type ToolChatRequest struct {
	Model     string
	System    string
	Messages  []Message
	Tools     []ToolDef
	MaxTokens int64
	MaxRounds int // default 4
}

type ToolChatResponse struct {
	Text      string
	Usage     Usage
	UsedTools bool
	Rounds    int
}

// ToolCaller is implemented by providers that support a multi-turn function-calling loop.
type ToolCaller interface {
	ChatWithTools(ctx context.Context, req ToolChatRequest, exec ToolExecutor) (ToolChatResponse, error)
}

const defaultToolRounds = 4

type gFuncCall struct {
	Name string          `json:"name"`
	Args json.RawMessage `json:"args,omitempty"`
}
type gFuncResp struct {
	Name     string         `json:"name"`
	Response map[string]any `json:"response"`
}
type gFuncDecl struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}
type gTool struct {
	FunctionDeclarations []gFuncDecl `json:"functionDeclarations"`
}

// geminiUnsupportedSchemaKeys are JSON-Schema keys Gemini's OpenAPI subset rejects.
var geminiUnsupportedSchemaKeys = map[string]bool{
	"$schema": true, "additionalProperties": true, "$id": true, "$ref": true, "definitions": true, "$defs": true,
	"default": true, "examples": true,
}

// GeminiSchema converts a JSON-Schema (Anthropic input_schema) into Gemini's Schema:
// upper-case types, unsupported keys dropped, recursing through properties/items.
func GeminiSchema(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		if geminiUnsupportedSchemaKeys[k] {
			continue
		}
		switch k {
		case "type":
			if s, ok := v.(string); ok {
				out[k] = strings.ToUpper(s)
				continue
			}
		case "properties":
			if props, ok := v.(map[string]any); ok {
				np := make(map[string]any, len(props))
				for pk, pv := range props {
					if pm, ok := pv.(map[string]any); ok {
						np[pk] = GeminiSchema(pm)
					} else {
						np[pk] = pv
					}
				}
				out[k] = np
				continue
			}
		case "items":
			if im, ok := v.(map[string]any); ok {
				out[k] = GeminiSchema(im)
				continue
			}
		}
		out[k] = v
	}
	return out
}

// GeminiFunctionDeclarations maps provider-neutral tools to Gemini functionDeclarations.
func GeminiFunctionDeclarations(tools []ToolDef) []gTool {
	if len(tools) == 0 {
		return nil
	}
	decls := make([]gFuncDecl, 0, len(tools))
	for _, t := range tools {
		params := t.Parameters
		if params == nil {
			params = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		decls = append(decls, gFuncDecl{Name: t.Name, Description: t.Description, Parameters: GeminiSchema(params)})
	}
	return []gTool{{FunctionDeclarations: decls}}
}

func toolResultPayload(result string, err error) map[string]any {
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	var v any
	if json.Unmarshal([]byte(result), &v) == nil {
		if m, ok := v.(map[string]any); ok {
			return m
		}
		return map[string]any{"result": v}
	}
	return map[string]any{"result": result}
}

// ChatWithTools runs Gemini's functionCall/functionResponse loop until the model answers
// with text or MaxRounds is exceeded. Usage is summed across rounds.
func (g *GeminiProvider) ChatWithTools(ctx context.Context, r ToolChatRequest, exec ToolExecutor) (ToolChatResponse, error) {
	rounds := r.MaxRounds
	if rounds <= 0 {
		rounds = defaultToolRounds
	}
	body := gReq{SystemInstruction: sysContent(r.System), GenerationConfig: genCfg(r.MaxTokens), Tools: GeminiFunctionDeclarations(r.Tools)}
	for _, m := range r.Messages {
		if strings.TrimSpace(m.Text) == "" {
			continue
		}
		role := "user"
		if m.Role == "assistant" {
			role = "model"
		}
		body.Contents = append(body.Contents, gContent{Role: role, Parts: []gPart{{Text: m.Text}}})
	}
	out := ToolChatResponse{Usage: Usage{Provider: g.ID(), Model: g.model(r.Model)}}
	for round := 0; round < rounds; round++ {
		var gr gResp
		body.rawOut = &gr
		_, u, err := g.generate(ctx, r.Model, body)
		out.Usage.InputTokens += u.InputTokens
		out.Usage.OutputTokens += u.OutputTokens
		out.Rounds = round + 1
		if err != nil {
			return out, err
		}
		if len(gr.Candidates) == 0 {
			return out, fmt.Errorf("gemini: no candidates")
		}
		modelTurn := gr.Candidates[0].Content
		var calls []gFuncCall
		var texts []string
		for _, p := range modelTurn.Parts {
			if p.FunctionCall != nil {
				calls = append(calls, *p.FunctionCall)
			} else if strings.TrimSpace(p.Text) != "" {
				texts = append(texts, p.Text)
			}
		}
		if len(calls) == 0 {
			out.Text = strings.TrimSpace(strings.Join(texts, "\n"))
			if out.Text == "" {
				return out, fmt.Errorf("gemini: empty completion")
			}
			return out, nil
		}
		out.UsedTools = true
		modelTurn.Role = "model"
		body.Contents = append(body.Contents, modelTurn)
		resp := gContent{Role: "user"}
		for _, c := range calls {
			args := c.Args
			if len(args) == 0 {
				args = json.RawMessage("{}")
			}
			res, rerr := exec(c.Name, args)
			resp.Parts = append(resp.Parts, gPart{FunctionResponse: &gFuncResp{Name: c.Name, Response: toolResultPayload(res, rerr)}})
		}
		body.Contents = append(body.Contents, resp)
	}
	return out, fmt.Errorf("gemini: tool loop exceeded %d rounds", rounds)
}

// ErrToolsUnsupported marks a chain link whose provider cannot run a tool loop; Run advances.
var ErrToolsUnsupported = errors.New("llm: provider does not support tool calling")

// ToolLink lets a caller keep a provider-specific tool path (e.g. the legacy Anthropic client
// with prompt caching + ResolveRouting). Return handled=false to use the generic ToolCaller.
type ToolLink func(ctx context.Context, p Provider, model string) (resp ToolChatResponse, handled bool, err error)

// RunToolChat runs the configured chain for fn (tenant → global → code default) with a
// multi-turn tool loop on each link, falling back on retryable errors. The returned
// response's Usage.Provider/Model are the ones actually used.
func RunToolChat(ctx context.Context, reg *Registry, tenantID string, fn Function, req ToolChatRequest,
	exec ToolExecutor, special ToolLink) (ToolChatResponse, RunResult, error) {
	var out ToolChatResponse
	res, err := reg.Run(ctx, tenantID, fn, func(ctx context.Context, p Provider, model string) error {
		if special != nil {
			r, handled, err := special(ctx, p, model)
			if handled {
				out = r
				return err
			}
		}
		tc, ok := p.(ToolCaller)
		if !ok {
			return fmt.Errorf("%w: %s", ErrToolsUnsupported, p.ID())
		}
		lr := req
		lr.Model = model
		r, err := tc.ChatWithTools(ctx, lr, exec)
		out = r
		return err
	})
	return out, res, err
}
