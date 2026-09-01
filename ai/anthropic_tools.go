package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"encore.dev/rlog"
	"github.com/anthropics/anthropic-sdk-go"
)

type toolUseCall struct {
	ID    string
	Name  string
	Input json.RawMessage
}

const maxCatalogToolRounds = 4

const catalogToolsTaskHint = "Tugas: balas singkat (maks 8 baris). Untuk produk/harga WAJIB panggil search_catalog atau get_product dulu. Jangan mengarang harga."

func catalogToolDefinitions() []anthropic.ToolUnionParam {
	return []anthropic.ToolUnionParam{
		{
			OfTool: &anthropic.ToolParam{
				Name:        catalogToolSearch,
				Description: anthropic.String("Cari produk di katalog resmi toko. Wajib dipakai sebelum menyebut nama atau harga produk."),
				InputSchema: anthropic.ToolInputSchemaParam{
					Properties: map[string]any{
						"query": map[string]any{
							"type":        "string",
							"description": "Kata kunci produk, misalnya boxer mono spot",
						},
						"limit": map[string]any{
							"type":        "integer",
							"description": "Maksimal jumlah hasil (1-10, default 5)",
						},
					},
					Required: []string{"query"},
				},
			},
		},
		{
			OfTool: &anthropic.ToolParam{
				Name:        catalogToolGetProduct,
				Description: anthropic.String("Ambil detail satu produk dari katalog berdasarkan nama atau kode internal."),
				InputSchema: anthropic.ToolInputSchemaParam{
					Properties: map[string]any{
						"ref": map[string]any{
							"type":        "string",
							"description": "Nama produk, cuplikan nama, atau kode internal",
						},
					},
					Required: []string{"ref"},
				},
			},
		},
	}
}

// GenerateSalesReplyWithCatalogTools — LLM + tool catalog (poin 1), fallback ke completion biasa jika gagal.
func (c *AnthropicClient) GenerateSalesReplyWithCatalogTools(
	ctx context.Context,
	model string,
	req SalesReplyRequest,
	exec *CatalogToolExecutor,
) (text string, usage CompletionUsage, usedTools bool, err error) {
	if strings.TrimSpace(model) == "" {
		model = c.model
	}
	model = ResolveAnthropicModel(model)

	toolReq := req
	if strings.TrimSpace(toolReq.TaskHint) == "" {
		toolReq.TaskHint = catalogToolsTaskHint
	}

	text, usage, usedTools, err = c.generateWithCatalogToolsOnce(ctx, model, toolReq, exec)
	if err == nil {
		return text, usage, usedTools, nil
	}
	if !isModelNotFoundErr(err) {
		rlog.Warn("anthropic catalog tools failed, falling back to plain completion", "err", err)
		plainReq := req
		if strings.TrimSpace(plainReq.TaskHint) == "" {
			plainReq.TaskHint = "Tugas: berikan satu balasan WhatsApp sales assistant yang aman, membantu, dan mendorong checkout. Produk/harga hanya dari katalog resmi."
		}
		plain, u, plainErr := c.GenerateReplyWithModel(ctx, model, plainReq)
		return plain, u, false, plainErr
	}
	var lastErr error
	for i, tryModel := range FallbackModels(model) {
		text, usage, usedTools, err = c.generateWithCatalogToolsOnce(ctx, tryModel, toolReq, exec)
		if err == nil {
			if i > 0 {
				rlog.Info("anthropic catalog tools succeeded after model fallback", "model", tryModel)
			}
			return text, usage, usedTools, nil
		}
		lastErr = err
		if !isModelNotFoundErr(err) {
			break
		}
	}
	plainReq := req
	if strings.TrimSpace(plainReq.TaskHint) == "" {
		plainReq.TaskHint = "Tugas: berikan satu balasan WhatsApp sales assistant yang aman, membantu, dan mendorong checkout. Produk/harga hanya dari katalog resmi."
	}
	plain, u, plainErr := c.GenerateReplyWithModel(ctx, model, plainReq)
	if plainErr == nil {
		return plain, u, false, nil
	}
	if lastErr != nil {
		return "", usage, false, fmt.Errorf("catalog tools and fallback failed: %w", lastErr)
	}
	return "", usage, false, plainErr
}

func (c *AnthropicClient) generateWithCatalogToolsOnce(
	ctx context.Context,
	model string,
	req SalesReplyRequest,
	exec *CatalogToolExecutor,
) (string, CompletionUsage, bool, error) {
	systemBlocks := buildSalesSystemBlocks(req)
	messages := buildSalesMessages(req)
	tools := catalogToolDefinitions()
	var usage CompletionUsage
	usedTools := false

	for round := 0; round < maxCatalogToolRounds; round++ {
		resp, err := c.callCatalogToolsRound(ctx, model, systemBlocks, tools, messages)
		if err != nil {
			if IsAnthropicRetryable(err) {
				delay := anthropicRetryDelay(0)
				select {
				case <-ctx.Done():
					return "", usage, usedTools, ctx.Err()
				case <-time.After(delay):
				}
				resp, err = c.callCatalogToolsRound(ctx, model, systemBlocks, tools, messages)
			}
			if err != nil {
				return "", usage, usedTools, err
			}
		}
		roundUsage := usageFromResponse(resp)
		addUsage(&usage, roundUsage)

		var textParts []string
		var toolUses []toolUseCall
		for _, block := range resp.Content {
			switch block.Type {
			case "text":
				if strings.TrimSpace(block.Text) != "" {
					textParts = append(textParts, block.Text)
				}
			case "tool_use":
				toolUses = append(toolUses, toolUseCall{
					ID: block.ID, Name: block.Name, Input: block.Input,
				})
			}
		}

		if len(toolUses) == 0 {
			out := strings.TrimSpace(strings.Join(textParts, "\n"))
			if out == "" {
				return "", usage, usedTools, fmt.Errorf("AI response kosong")
			}
			if resp.StopReason == anthropic.StopReasonMaxTokens {
				rlog.Warn("anthropic catalog tools truncated by max_tokens", "model", model, "round", round)
			}
			out = finalizeAnthropicReply(out, resp.StopReason)
			return out, usage, usedTools, nil
		}

		usedTools = true
		assistant := resp.ToParam()
		messages = append(messages, assistant)

		var resultBlocks []anthropic.ContentBlockParamUnion
		for _, tu := range toolUses {
			result, runErr := exec.Run(tu.Name, tu.Input)
			if runErr != nil {
				result = fmt.Sprintf(`{"error":%q}`, runErr.Error())
				resultBlocks = append(resultBlocks, anthropic.NewToolResultBlock(tu.ID, result, true))
				continue
			}
			resultBlocks = append(resultBlocks, anthropic.NewToolResultBlock(tu.ID, result, false))
		}
		messages = append(messages, anthropic.NewUserMessage(resultBlocks...))
	}
	return "", usage, usedTools, fmt.Errorf("catalog tool loop exceeded %d rounds", maxCatalogToolRounds)
}

func (c *AnthropicClient) callCatalogToolsRound(
	ctx context.Context,
	model string,
	systemBlocks []anthropic.TextBlockParam,
	tools []anthropic.ToolUnionParam,
	messages []anthropic.MessageParam,
) (*anthropic.Message, error) {
	return c.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:       anthropic.Model(model),
		MaxTokens:   c.maxTok,
		Temperature: anthropic.Float(0.2),
		System:      systemBlocks,
		Tools:       tools,
		Messages:    messages,
	})
}
