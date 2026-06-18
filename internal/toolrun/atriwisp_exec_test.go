package toolrun

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fujiwarashigure/ATRI-X/internal/llm"
)

func TestExecuteAtriwispToolPostsArguments(t *testing.T) {
	var received atriwispToolRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodPost {
			t.Fatalf("method = %s", req.Method)
		}
		if err := json.NewDecoder(req.Body).Decode(&received); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"content":"ok"}`))
	}))
	defer server.Close()

	call := llm.ToolCallRequest{ID: "call-1", Name: "atriwisp_watcher_server_status", Arguments: `{"detail":true}`}
	result := executeAtriwispTool(context.Background(), call, ResolvedTool{Cached: &CachedTool{Name: "server_status", CanonicalName: call.Name, Source: SourceKindAtriwisp, EventKey: "watcher/source/id", Endpoint: server.URL}})
	if result.Err != nil {
		t.Fatalf("executeAtriwispTool: %v", result.Err)
	}
	if got := llm.SegmentsContentText(result.Message.Segments); got != "ok" {
		t.Fatalf("content = %q", got)
	}
	if received.Tool != "server_status" || received.EventKey != "watcher/source/id" || string(received.Arguments) != `{"detail":true}` {
		t.Fatalf("received = %#v", received)
	}
}

func TestExecuteAtriwispToolReturnsHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		http.Error(w, "boom", http.StatusBadGateway)
	}))
	defer server.Close()

	call := llm.ToolCallRequest{ID: "call-1", Name: "atriwisp_watcher_server_status", Arguments: `{}`}
	result := executeAtriwispTool(context.Background(), call, ResolvedTool{Cached: &CachedTool{Name: "server_status", CanonicalName: call.Name, Source: SourceKindAtriwisp, Endpoint: server.URL}})
	if result.Err == nil {
		t.Fatal("expected HTTP error")
	}
}
