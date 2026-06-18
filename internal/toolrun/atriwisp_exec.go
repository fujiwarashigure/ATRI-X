package toolrun

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/fujiwarashigure/ATRI-X/internal/llm"
	"github.com/fujiwarashigure/ATRI-X/internal/tool"
)

type atriwispToolRequest struct {
	Tool      string          `json:"tool"`
	EventKey  string          `json:"event_key,omitempty"`
	Arguments json.RawMessage `json:"arguments"`
}

type atriwispToolResponse struct {
	Content string `json:"content"`
	Text    string `json:"text"`
	Result  string `json:"result"`
	Error   string `json:"error"`
}

func executeAtriwispTool(ctx context.Context, call llm.ToolCallRequest, resolved ResolvedTool) ExecutionResult {
	message := llm.LLMMessage{Role: llm.RoleTool, Name: call.Name, ToolCallID: call.ID}
	cached := resolved.Cached
	if cached == nil {
		return executionError(call, message, fmt.Errorf("Atriwisp tool metadata is missing"))
	}
	timeout := time.Duration(cached.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	toolCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	arguments := json.RawMessage(strings.TrimSpace(call.Arguments))
	if len(arguments) == 0 {
		arguments = json.RawMessage(`{}`)
	}
	payload, err := json.Marshal(atriwispToolRequest{Tool: cached.Name, EventKey: cached.EventKey, Arguments: arguments})
	if err != nil {
		return executionError(call, message, err)
	}
	req, err := http.NewRequestWithContext(toolCtx, http.MethodPost, cached.Endpoint, bytes.NewReader(payload))
	if err != nil {
		return executionError(call, message, err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return executionError(call, message, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return executionError(call, message, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return executionError(call, message, fmt.Errorf("Atriwisp tool HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body))))
	}
	content := strings.TrimSpace(string(body))
	var parsed atriwispToolResponse
	if err := json.Unmarshal(body, &parsed); err == nil {
		if strings.TrimSpace(parsed.Error) != "" {
			return executionError(call, message, fmt.Errorf("Atriwisp tool error: %s", strings.TrimSpace(parsed.Error)))
		}
		content = firstAtriwispResult(parsed.Content, parsed.Text, parsed.Result, content)
	}
	result := &tool.Result{Content: content}
	message.Segments = result.LLMSegments()
	return ExecutionResult{Call: call, Message: message, Result: result}
}

func firstAtriwispResult(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
