package atrinis

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fujiwarashigure/ATRI-X/internal/atriena"
	"github.com/fujiwarashigure/ATRI-X/internal/background"
	"github.com/fujiwarashigure/ATRI-X/internal/config"
	"github.com/fujiwarashigure/ATRI-X/internal/delivery"
	"github.com/fujiwarashigure/ATRI-X/internal/storage"
	"github.com/fujiwarashigure/ATRI-X/internal/storage/sqlite"
	"github.com/fujiwarashigure/ATRI-X/internal/toolrun"
)

func TestHandleRecordCreatesEventAndRejectsDuplicate(t *testing.T) {
	service, cleanup := newTestService(t, nil)
	defer cleanup()

	req := testRequest(ModeRecord)
	resp, err := service.Handle(context.Background(), "secret", req)
	if err != nil {
		t.Fatalf("Handle record: %v", err)
	}
	if !resp.Accepted || resp.Duplicate || resp.Status != StatusCompleted {
		t.Fatalf("response = %#v", resp)
	}

	resp, err = service.Handle(context.Background(), "secret", req)
	if err != nil {
		t.Fatalf("Handle duplicate: %v", err)
	}
	if !resp.Accepted || !resp.Duplicate || resp.Status != StatusDuplicate {
		t.Fatalf("duplicate response = %#v", resp)
	}
}

func TestEventAttrsIncludeTags(t *testing.T) {
	service, cleanup := newTestService(t, nil)
	defer cleanup()

	req := testRequest(ModeRecord)
	event, err := service.prepareEvent(atriena.Origin{Kind: atriena.OriginInternal, Name: "home"}, req)
	if err != nil {
		t.Fatalf("prepareEvent: %v", err)
	}
	attrs := service.eventAttrs(event)
	for i := 0; i+1 < len(attrs); i += 2 {
		if attrs[i] == "tags" && attrs[i+1] == `["test"]` {
			return
		}
	}
	t.Fatalf("tags missing from attrs: %#v", attrs)
}

func TestHandleRejectsUnauthorizedToken(t *testing.T) {
	service, cleanup := newTestService(t, nil)
	defer cleanup()

	resp, err := service.Handle(context.Background(), "wrong", testRequest(ModeRecord))
	if err == nil {
		t.Fatal("expected unauthorized error")
	}
	if resp.Accepted || resp.Status != StatusFailed {
		t.Fatalf("response = %#v", resp)
	}
}

func TestHandleAllowsUnconfiguredAtriwisp(t *testing.T) {
	service, cleanup := newTestService(t, nil)
	defer cleanup()

	req := testRequest(ModeRecord)
	req.Atriwisp.Name = "unknown"
	resp, err := service.Handle(context.Background(), "secret", req)
	if err != nil {
		t.Fatalf("Handle unconfigured atriwisp: %v", err)
	}
	if !resp.Accepted || resp.Status != StatusCompleted {
		t.Fatalf("response = %#v", resp)
	}
}

func TestHandleAllowsConfiguredAtriwispWithoutEnabled(t *testing.T) {
	service, cleanup := newTestService(t, nil)
	defer cleanup()

	req := testRequest(ModeRecord)
	req.Atriwisp.Name = "configured"
	resp, err := service.Handle(context.Background(), "secret", req)
	if err != nil {
		t.Fatalf("Handle configured atriwisp without enabled: %v", err)
	}
	if !resp.Accepted || resp.Status != StatusCompleted {
		t.Fatalf("response = %#v", resp)
	}
}

func TestHandleAllowsExplicitEnabledAtriwisp(t *testing.T) {
	service, cleanup := newTestService(t, nil)
	defer cleanup()

	req := testRequest(ModeRecord)
	req.Atriwisp.Name = "enabled"
	resp, err := service.Handle(context.Background(), "secret", req)
	if err != nil {
		t.Fatalf("Handle explicitly enabled atriwisp: %v", err)
	}
	if !resp.Accepted || resp.Status != StatusCompleted {
		t.Fatalf("response = %#v", resp)
	}
}

func TestHandleRejectsDisabledAtriwisp(t *testing.T) {
	service, cleanup := newTestService(t, nil)
	defer cleanup()

	req := testRequest(ModeRecord)
	req.Atriwisp.Name = "disabled"
	resp, err := service.Handle(context.Background(), "secret", req)
	if err == nil {
		t.Fatal("expected disabled atriwisp error")
	}
	if resp.Accepted || resp.Status != StatusFailed {
		t.Fatalf("response = %#v", resp)
	}
}

func TestHandleRejectsTokenNotAllowedForAtriwisp(t *testing.T) {
	service, cleanup := newTestService(t, nil)
	defer cleanup()

	req := testRequest(ModeRecord)
	resp, err := service.Handle(context.Background(), "alt-secret", req)
	if err == nil {
		t.Fatal("expected token restriction error")
	}
	if resp.Accepted || resp.Status != StatusFailed {
		t.Fatalf("response = %#v", resp)
	}
}

func TestHandleLLMQueuesEvent(t *testing.T) {
	service, cleanup := newTestService(t, nil)
	defer cleanup()
	var queued QueuedLLMEvent
	service.SetLLMEnqueuer(func(ctx context.Context, event QueuedLLMEvent) error {
		queued = event
		return nil
	})

	req := testRequest(ModeLLM)
	resp, err := service.Handle(context.Background(), "secret", req)
	if err != nil {
		t.Fatalf("Handle llm: %v", err)
	}
	if resp.Status != StatusQueued || queued.EventID == "" || queued.Event.EventKey == "" {
		t.Fatalf("response = %#v queued = %#v", resp, queued)
	}
	record, err := service.store.AtrinisEvents().GetByKey(context.Background(), req.Atriwisp.Name, req.Source, req.ID)
	if err != nil {
		t.Fatalf("GetByKey: %v", err)
	}
	if record.Status != StatusQueued {
		t.Fatalf("status = %q", record.Status)
	}
}

func TestHandleAllowsConfiguredInternalTool(t *testing.T) {
	service, cleanup := newTestService(t, nil)
	defer cleanup()

	req := testRequest(ModeLLM)
	req.ToolListNames = []string{"shell"}
	resp, err := service.Handle(context.Background(), "secret", req)
	if err != nil {
		t.Fatalf("Handle allowed tool: %v", err)
	}
	if resp.Status != StatusQueued {
		t.Fatalf("response = %#v", resp)
	}
}

func TestHandleRejectsDisallowedInternalTool(t *testing.T) {
	service, cleanup := newTestService(t, nil)
	defer cleanup()

	req := testRequest(ModeLLM)
	req.ToolListNames = []string{"cron"}
	resp, err := service.Handle(context.Background(), "secret", req)
	if err == nil {
		t.Fatal("expected disallowed internal tool error")
	}
	if resp.Accepted || !strings.Contains(resp.Error, "not allowed") {
		t.Fatalf("response = %#v", resp)
	}
}

func TestHandleAllowsExternalToolsByDefault(t *testing.T) {
	service, cleanup := newTestService(t, nil)
	defer cleanup()

	req := testRequest(ModeLLM)
	req.Tools = []toolrun.AtriwispToolDeclaration{testExternalTool("server_status")}
	resp, err := service.Handle(context.Background(), "secret", req)
	if err != nil {
		t.Fatalf("Handle external tool: %v", err)
	}
	if resp.Status != StatusQueued {
		t.Fatalf("response = %#v", resp)
	}
	record, err := service.store.AtrinisEvents().GetByKey(context.Background(), req.Atriwisp.Name, req.Source, req.ID)
	if err != nil {
		t.Fatalf("GetByKey: %v", err)
	}
	if record.ToolDeclarations == "" || record.ToolHash == "" {
		t.Fatalf("tool declaration not persisted: %#v", record)
	}
}

func TestHandleRejectsDisabledExternalTool(t *testing.T) {
	service, cleanup := newTestService(t, nil)
	defer cleanup()

	req := testRequest(ModeLLM)
	req.Tools = []toolrun.AtriwispToolDeclaration{testExternalTool("danger_tool")}
	resp, err := service.Handle(context.Background(), "secret", req)
	if err == nil {
		t.Fatal("expected disabled external tool error")
	}
	if resp.Accepted || !strings.Contains(resp.Error, "disabled") {
		t.Fatalf("response = %#v", resp)
	}
}

func TestRunLLMEventUsesModelSlot(t *testing.T) {
	runner := &fakeBackgroundRunner{text: `{"completed":true,"need_report":false,"report":""}`}
	service, cleanup := newTestServiceWithRunner(t, runner, nil)
	defer cleanup()
	var queued QueuedLLMEvent
	service.SetLLMEnqueuer(func(ctx context.Context, event QueuedLLMEvent) error {
		queued = event
		return nil
	})

	req := testRequest(ModeLLM)
	req.ModelSlot = "atriwisp2"
	if _, err := service.Handle(context.Background(), "secret", req); err != nil {
		t.Fatalf("Handle llm: %v", err)
	}
	if err := service.RunLLMEvent(context.Background(), queued.Event, queued.EventID); err != nil {
		t.Fatalf("RunLLMEvent: %v", err)
	}
	if len(runner.requests) != 1 || runner.requests[0].ModelProvider != "provider-atriwisp2" || runner.requests[0].Model != "model-atriwisp2" {
		t.Fatalf("requests = %#v", runner.requests)
	}
}

func TestRunLLMEventFallsBackToWorkModel(t *testing.T) {
	runner := &fakeBackgroundRunner{text: `{"completed":true,"need_report":false,"report":""}`}
	service, cleanup := newTestServiceWithRunner(t, runner, nil)
	defer cleanup()
	var queued QueuedLLMEvent
	service.SetLLMEnqueuer(func(ctx context.Context, event QueuedLLMEvent) error {
		queued = event
		return nil
	})

	req := testRequest(ModeLLM)
	if _, err := service.Handle(context.Background(), "secret", req); err != nil {
		t.Fatalf("Handle llm: %v", err)
	}
	if err := service.RunLLMEvent(context.Background(), queued.Event, queued.EventID); err != nil {
		t.Fatalf("RunLLMEvent: %v", err)
	}
	if len(runner.requests) != 1 || runner.requests[0].ModelProvider != "provider-work" || runner.requests[0].Model != "model-work" {
		t.Fatalf("requests = %#v", runner.requests)
	}
}

func TestRunLLMEventFallsBackWhenSlotUnconfigured(t *testing.T) {
	runner := &fakeBackgroundRunner{text: `{"completed":true,"need_report":false,"report":""}`}
	service, cleanup := newTestServiceWithRunner(t, runner, nil)
	defer cleanup()
	service.resolveModel = func(slot string) config.ModelSelection {
		if slot == "work" {
			return config.ModelSelection{Provider: "provider-work", Model: "model-work"}
		}
		return config.ModelSelection{}
	}
	var queued QueuedLLMEvent
	service.SetLLMEnqueuer(func(ctx context.Context, event QueuedLLMEvent) error {
		queued = event
		return nil
	})

	req := testRequest(ModeLLM)
	req.ModelSlot = "atriwisp3"
	if _, err := service.Handle(context.Background(), "secret", req); err != nil {
		t.Fatalf("Handle llm: %v", err)
	}
	if err := service.RunLLMEvent(context.Background(), queued.Event, queued.EventID); err != nil {
		t.Fatalf("RunLLMEvent: %v", err)
	}
	if len(runner.requests) != 1 || runner.requests[0].ModelProvider != "provider-work" || runner.requests[0].Model != "model-work" {
		t.Fatalf("requests = %#v", runner.requests)
	}
}

func TestHandleRejectsInvalidModelSlot(t *testing.T) {
	service, cleanup := newTestService(t, nil)
	defer cleanup()

	req := testRequest(ModeLLM)
	req.ModelSlot = "chat"
	resp, err := service.Handle(context.Background(), "secret", req)
	if err == nil {
		t.Fatal("expected invalid model_slot error")
	}
	if resp.Accepted || !strings.Contains(resp.Error, "unsupported model_slot") {
		t.Fatalf("response = %#v", resp)
	}
}

func TestHandleRejectsAtriwispNameWithDot(t *testing.T) {
	service, cleanup := newTestService(t, nil)
	defer cleanup()

	req := testRequest(ModeLLM)
	req.Atriwisp.Name = "foo.bar"
	resp, err := service.Handle(context.Background(), "secret", req)
	if err == nil {
		t.Fatal("expected invalid atriwisp.name error")
	}
	if resp.Accepted || !strings.Contains(resp.Error, "atriwisp.name") {
		t.Fatalf("response = %#v", resp)
	}
}

func TestRunLLMEventMapsReportNoticeToBackgroundMessage(t *testing.T) {
	runner := &fakeBackgroundRunner{text: `{"completed":true,"need_report":true,"report":"处理完成"}`}
	var service *Service
	service, cleanup := newTestServiceWithRunner(t, runner, func(ctx context.Context, target delivery.Target, outputs []delivery.Output) (delivery.Receipt, error) {
		if target.Platform != "qq-onebot" || target.PrivateUserID != "1001" {
			t.Fatalf("target = %#v", target)
		}
		return delivery.Receipt{PlatformMessageIDs: []string{"notice-1"}}, nil
	})
	defer cleanup()
	bgSession := &storage.Session{ID: "bg-session", OwnerID: "atrinis:home", Platform: "qq-onebot", PlatformScopeID: "atrinis:watcher:source:event-1", Mode: storage.SessionModeWork, Status: storage.SessionStatusActive, Title: "atrinis"}
	if err := service.store.Sessions().Create(context.Background(), bgSession); err != nil {
		t.Fatalf("create background session: %v", err)
	}
	if err := service.store.Messages().Append(context.Background(), &storage.Message{ID: "bg-message", SessionID: bgSession.ID, Role: storage.RoleAssistant, Content: "处理完成"}); err != nil {
		t.Fatalf("append background message: %v", err)
	}
	var queued QueuedLLMEvent
	service.SetLLMEnqueuer(func(ctx context.Context, event QueuedLLMEvent) error {
		queued = event
		return nil
	})

	req := testRequest(ModeLLM)
	req.Targets = []Target{{Platform: "qq-onebot", Type: "private", ID: "1001"}}
	if _, err := service.Handle(context.Background(), "secret", req); err != nil {
		t.Fatalf("Handle llm: %v", err)
	}
	if err := service.RunLLMEvent(context.Background(), queued.Event, queued.EventID); err != nil {
		t.Fatalf("RunLLMEvent: %v", err)
	}
	msg, err := service.store.Messages().FindByPlatformMessage(context.Background(), "qq-onebot", "private:1001", "notice-1")
	if err != nil {
		t.Fatalf("FindByPlatformMessage: %v", err)
	}
	if msg.ID != "bg-message" || msg.SessionID != "bg-session" {
		t.Fatalf("mapped message = %#v", msg)
	}
}

func TestRunLLMEventCompletesAndReports(t *testing.T) {
	runner := &fakeBackgroundRunner{text: `{"completed":true,"need_report":true,"report":"处理完成"}`}
	sent := []string{}
	service, cleanup := newTestServiceWithRunner(t, runner, func(ctx context.Context, target delivery.Target, outputs []delivery.Output) (delivery.Receipt, error) {
		if len(outputs) > 0 {
			sent = append(sent, target.Platform+":"+outputs[0].Text)
		}
		return delivery.Receipt{}, nil
	})
	defer cleanup()
	var queued QueuedLLMEvent
	service.SetLLMEnqueuer(func(ctx context.Context, event QueuedLLMEvent) error {
		queued = event
		return nil
	})

	req := testRequest(ModeLLM)
	req.Targets = []Target{{Platform: "cli"}}
	if _, err := service.Handle(context.Background(), "secret", req); err != nil {
		t.Fatalf("Handle llm: %v", err)
	}
	if err := service.RunLLMEvent(context.Background(), queued.Event, queued.EventID); err != nil {
		t.Fatalf("RunLLMEvent: %v", err)
	}
	record, err := service.store.AtrinisEvents().GetByKey(context.Background(), req.Atriwisp.Name, req.Source, req.ID)
	if err != nil {
		t.Fatalf("GetByKey: %v", err)
	}
	if record.Status != StatusCompleted || record.SessionID != "bg-session" || !strings.Contains(record.Result, "处理完成") {
		t.Fatalf("record = %#v", record)
	}
	if len(sent) != 1 || sent[0] != "cli:处理完成" {
		t.Fatalf("sent = %#v", sent)
	}
	if len(runner.requests) != 1 || runner.requests[0].Kind != background.KindAtrinis || runner.requests[0].SandboxSubdir != "atrinis/watcher" {
		t.Fatalf("requests = %#v", runner.requests)
	}
}

func TestRunLLMEventDoesNotCompleteBeforeReportDelivery(t *testing.T) {
	runner := &fakeBackgroundRunner{text: `{"completed":true,"need_report":true,"report":"处理完成"}`}
	entered := make(chan struct{})
	release := make(chan struct{})
	released := false
	defer func() {
		if !released {
			close(release)
		}
	}()
	service, cleanup := newTestServiceWithRunner(t, runner, func(ctx context.Context, target delivery.Target, outputs []delivery.Output) (delivery.Receipt, error) {
		close(entered)
		select {
		case <-release:
			return delivery.Receipt{PlatformMessageIDs: []string{"notice-1"}}, nil
		case <-ctx.Done():
			return delivery.Receipt{}, ctx.Err()
		}
	})
	defer cleanup()
	var queued QueuedLLMEvent
	service.SetLLMEnqueuer(func(ctx context.Context, event QueuedLLMEvent) error {
		queued = event
		return nil
	})

	req := testRequest(ModeLLM)
	if _, err := service.Handle(context.Background(), "secret", req); err != nil {
		t.Fatalf("Handle llm: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		done <- service.RunLLMEvent(context.Background(), queued.Event, queued.EventID)
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("report sender was not called")
	}
	record, err := service.store.AtrinisEvents().Get(context.Background(), queued.EventID)
	if err != nil {
		t.Fatalf("Get event while sending: %v", err)
	}
	if record.Status != StatusDelivering {
		t.Fatalf("status while sending = %q, want %q", record.Status, StatusDelivering)
	}

	close(release)
	released = true
	if err := <-done; err != nil {
		t.Fatalf("RunLLMEvent: %v", err)
	}
	record, err = service.store.AtrinisEvents().Get(context.Background(), queued.EventID)
	if err != nil {
		t.Fatalf("Get completed event: %v", err)
	}
	if record.Status != StatusCompleted {
		t.Fatalf("completed status = %q", record.Status)
	}
	deliveries, err := service.store.AtrinisEvents().ListReportDeliveries(context.Background(), queued.EventID)
	if err != nil {
		t.Fatalf("ListReportDeliveries: %v", err)
	}
	if len(deliveries) != 1 || deliveries[0].Status != storage.AtrinisReportDeliveryDelivered || !strings.Contains(deliveries[0].Receipt, "notice-1") {
		t.Fatalf("deliveries = %#v", deliveries)
	}
}

func TestRunLLMEventRetriesOnlyIncompleteReportDeliveries(t *testing.T) {
	runner := &fakeBackgroundRunner{text: `{"completed":true,"need_report":true,"report":"处理完成"}`}
	calls := 0
	service, cleanup := newTestServiceWithRunner(t, runner, func(ctx context.Context, target delivery.Target, outputs []delivery.Output) (delivery.Receipt, error) {
		calls++
		if calls == 2 {
			return delivery.Receipt{}, errors.New("temporary delivery failure")
		}
		return delivery.Receipt{PlatformMessageIDs: []string{"notice-" + target.Platform}}, nil
	})
	defer cleanup()
	var queued QueuedLLMEvent
	service.SetLLMEnqueuer(func(ctx context.Context, event QueuedLLMEvent) error {
		queued = event
		return nil
	})

	req := testRequest(ModeLLM)
	req.Targets = []Target{{Platform: "cli"}, {Platform: "qqonebot"}}
	if _, err := service.Handle(context.Background(), "secret", req); err != nil {
		t.Fatalf("Handle llm: %v", err)
	}
	if err := service.RunLLMEvent(context.Background(), queued.Event, queued.EventID); err == nil {
		t.Fatal("expected report delivery failure")
	}
	record, err := service.store.AtrinisEvents().Get(context.Background(), queued.EventID)
	if err != nil {
		t.Fatalf("Get failed delivery event: %v", err)
	}
	if record.Status != StatusResultReady || !strings.Contains(record.Error, "temporary delivery failure") {
		t.Fatalf("record after failure = %#v", record)
	}
	deliveries, err := service.store.AtrinisEvents().ListReportDeliveries(context.Background(), queued.EventID)
	if err != nil {
		t.Fatalf("ListReportDeliveries after failure: %v", err)
	}
	if len(deliveries) != 2 || deliveries[0].Status != storage.AtrinisReportDeliveryDelivered || deliveries[1].Status != storage.AtrinisReportDeliveryFailed {
		t.Fatalf("deliveries after failure = %#v", deliveries)
	}

	if err := service.recoverReports(context.Background(), false); err != nil {
		t.Fatalf("recoverReports: %v", err)
	}
	record, err = service.store.AtrinisEvents().Get(context.Background(), queued.EventID)
	if err != nil {
		t.Fatalf("Get recovered event: %v", err)
	}
	if record.Status != StatusCompleted || calls != 3 {
		t.Fatalf("recovered record = %#v, send calls = %d", record, calls)
	}
	deliveries, err = service.store.AtrinisEvents().ListReportDeliveries(context.Background(), queued.EventID)
	if err != nil {
		t.Fatalf("ListReportDeliveries after recovery: %v", err)
	}
	if deliveries[0].Attempts != 1 || deliveries[1].Attempts != 2 {
		t.Fatalf("delivery attempts = %d, %d", deliveries[0].Attempts, deliveries[1].Attempts)
	}
}

func TestRunLLMEventRetriesWhenReceiptPersistenceFails(t *testing.T) {
	runner := &fakeBackgroundRunner{text: `{"completed":true,"need_report":true,"report":"处理完成"}`}
	calls := 0
	service, cleanup := newTestServiceWithRunner(t, runner, func(ctx context.Context, target delivery.Target, outputs []delivery.Output) (delivery.Receipt, error) {
		calls++
		return delivery.Receipt{PlatformMessageIDs: []string{"notice-1"}}, nil
	})
	defer cleanup()
	failingRepo := &failOnceReceiptRepository{AtrinisEventRepository: service.store.AtrinisEvents()}
	service.store = atrinisRepositoryStore{Store: service.store, repo: failingRepo}
	var queued QueuedLLMEvent
	service.SetLLMEnqueuer(func(ctx context.Context, event QueuedLLMEvent) error {
		queued = event
		return nil
	})

	req := testRequest(ModeLLM)
	if _, err := service.Handle(context.Background(), "secret", req); err != nil {
		t.Fatalf("Handle llm: %v", err)
	}
	if err := service.RunLLMEvent(context.Background(), queued.Event, queued.EventID); err == nil {
		t.Fatal("expected receipt persistence failure")
	}
	record, err := service.store.AtrinisEvents().Get(context.Background(), queued.EventID)
	if err != nil {
		t.Fatalf("Get event after receipt failure: %v", err)
	}
	if record.Status != StatusResultReady || calls != 1 {
		t.Fatalf("record after receipt failure = %#v, calls = %d", record, calls)
	}

	if err := service.recoverReports(context.Background(), false); err != nil {
		t.Fatalf("recoverReports: %v", err)
	}
	record, err = service.store.AtrinisEvents().Get(context.Background(), queued.EventID)
	if err != nil {
		t.Fatalf("Get recovered event: %v", err)
	}
	if record.Status != StatusCompleted || calls != 2 {
		t.Fatalf("recovered record = %#v, calls = %d", record, calls)
	}
	deliveries, err := service.store.AtrinisEvents().ListReportDeliveries(context.Background(), queued.EventID)
	if err != nil {
		t.Fatalf("ListReportDeliveries: %v", err)
	}
	if len(deliveries) != 1 || deliveries[0].Attempts != 2 || deliveries[0].Status != storage.AtrinisReportDeliveryDelivered {
		t.Fatalf("deliveries = %#v", deliveries)
	}
}

func TestRecoverReportsResetsInterruptedDeliveringEvent(t *testing.T) {
	sent := 0
	service, cleanup := newTestService(t, func(ctx context.Context, target delivery.Target, outputs []delivery.Output) (delivery.Receipt, error) {
		sent++
		return delivery.Receipt{}, nil
	})
	defer cleanup()
	var queued QueuedLLMEvent
	service.SetLLMEnqueuer(func(ctx context.Context, event QueuedLLMEvent) error {
		queued = event
		return nil
	})
	req := testRequest(ModeLLM)
	if _, err := service.Handle(context.Background(), "secret", req); err != nil {
		t.Fatalf("Handle llm: %v", err)
	}
	parsed := background.JSONResult{Completed: true, NeedReport: true, Report: "处理完成"}
	resultJSON, err := json.Marshal(parsed)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := service.prepareReport(context.Background(), queued.Event, queued.EventID, string(resultJSON), "bg-session", "bg-message", parsed)
	if err != nil || !prepared {
		t.Fatalf("prepareReport = %v, %v", prepared, err)
	}
	claimed, err := service.store.AtrinisEvents().ClaimReport(context.Background(), queued.EventID, StatusResultReady, StatusDelivering)
	if err != nil || !claimed {
		t.Fatalf("ClaimReport = %v, %v", claimed, err)
	}

	if err := service.recoverReports(context.Background(), true); err != nil {
		t.Fatalf("recoverReports: %v", err)
	}
	record, err := service.store.AtrinisEvents().Get(context.Background(), queued.EventID)
	if err != nil {
		t.Fatalf("Get recovered event: %v", err)
	}
	if record.Status != StatusCompleted || sent != 1 {
		t.Fatalf("recovered record = %#v, sent = %d", record, sent)
	}
}

func TestRecoverReportsDoesNotResendPersistedReceipt(t *testing.T) {
	sent := 0
	service, cleanup := newTestService(t, func(ctx context.Context, target delivery.Target, outputs []delivery.Output) (delivery.Receipt, error) {
		sent++
		return delivery.Receipt{}, nil
	})
	defer cleanup()
	var queued QueuedLLMEvent
	service.SetLLMEnqueuer(func(ctx context.Context, event QueuedLLMEvent) error {
		queued = event
		return nil
	})
	req := testRequest(ModeLLM)
	if _, err := service.Handle(context.Background(), "secret", req); err != nil {
		t.Fatalf("Handle llm: %v", err)
	}
	parsed := background.JSONResult{Completed: true, NeedReport: true, Report: "处理完成"}
	resultJSON, err := json.Marshal(parsed)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := service.prepareReport(context.Background(), queued.Event, queued.EventID, string(resultJSON), "bg-session", "bg-message", parsed)
	if err != nil || !prepared {
		t.Fatalf("prepareReport = %v, %v", prepared, err)
	}
	repo := service.store.AtrinisEvents()
	claimed, err := repo.ClaimReport(context.Background(), queued.EventID, StatusResultReady, StatusDelivering)
	if err != nil || !claimed {
		t.Fatalf("ClaimReport = %v, %v", claimed, err)
	}
	deliveries, err := repo.ListReportDeliveries(context.Background(), queued.EventID)
	if err != nil || len(deliveries) != 1 {
		t.Fatalf("ListReportDeliveries = %#v, %v", deliveries, err)
	}
	if err := repo.StartReportDelivery(context.Background(), deliveries[0].ID); err != nil {
		t.Fatalf("StartReportDelivery: %v", err)
	}
	if err := repo.MarkReportDeliveryDelivered(context.Background(), deliveries[0].ID, `{"PlatformMessageIDs":["notice-1"]}`); err != nil {
		t.Fatalf("MarkReportDeliveryDelivered: %v", err)
	}

	if err := service.recoverReports(context.Background(), true); err != nil {
		t.Fatalf("recoverReports: %v", err)
	}
	record, err := repo.Get(context.Background(), queued.EventID)
	if err != nil {
		t.Fatalf("Get recovered event: %v", err)
	}
	if record.Status != StatusCompleted || sent != 0 {
		t.Fatalf("recovered record = %#v, sent = %d", record, sent)
	}
}

func TestRunLLMEventReportsSegmentsByRelativePath(t *testing.T) {
	runner := &fakeBackgroundRunner{text: `{"completed":true,"need_report":true,"report":"见图","report_segments":[{"type":"image","url":"chart.png"}]}`}
	sent := []delivery.Kind{}
	service, cleanup := newTestServiceWithRunner(t, runner, func(ctx context.Context, target delivery.Target, outputs []delivery.Output) (delivery.Receipt, error) {
		if len(outputs) > 0 {
			sent = append(sent, outputs[0].Kind)
		}
		return delivery.Receipt{}, nil
	})
	defer cleanup()
	if err := os.MkdirAll(filepath.Join(service.sandboxRoot, "atrinis", "watcher"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(service.sandboxRoot, "atrinis", "watcher", "chart.png"), []byte("png"), 0644); err != nil {
		t.Fatal(err)
	}
	var queued QueuedLLMEvent
	service.SetLLMEnqueuer(func(ctx context.Context, event QueuedLLMEvent) error {
		queued = event
		return nil
	})
	req := testRequest(ModeLLM)
	req.Targets = []Target{{Platform: "cli"}}
	if _, err := service.Handle(context.Background(), "secret", req); err != nil {
		t.Fatalf("Handle llm: %v", err)
	}
	if err := service.RunLLMEvent(context.Background(), queued.Event, queued.EventID); err != nil {
		t.Fatalf("RunLLMEvent: %v", err)
	}
	if len(sent) != 1 || sent[0] != delivery.KindText {
		t.Fatalf("sent kinds = %#v", sent)
	}
}

func TestRunLLMEventRejectsAbsoluteReportSegmentPath(t *testing.T) {
	runner := &fakeBackgroundRunner{text: `{"completed":true,"need_report":true,"report":"见图","report_segments":[{"type":"image","url":"/tmp/chart.png"}]}`}
	service, cleanup := newTestServiceWithRunner(t, runner, func(ctx context.Context, target delivery.Target, outputs []delivery.Output) (delivery.Receipt, error) {
		return delivery.Receipt{}, nil
	})
	defer cleanup()
	var queued QueuedLLMEvent
	service.SetLLMEnqueuer(func(ctx context.Context, event QueuedLLMEvent) error {
		queued = event
		return nil
	})
	req := testRequest(ModeLLM)
	req.Targets = []Target{{Platform: "cli"}}
	if _, err := service.Handle(context.Background(), "secret", req); err != nil {
		t.Fatalf("Handle llm: %v", err)
	}
	if err := service.RunLLMEvent(context.Background(), queued.Event, queued.EventID); err == nil {
		t.Fatal("expected absolute report segment path error")
	}
}

func TestRunLLMEventReportsFailedResultWhenRequested(t *testing.T) {
	runner := &fakeBackgroundRunner{text: `{"completed":false,"need_report":true,"report":"创建失败：工具权限不足"}`}
	sent := []string{}
	service, cleanup := newTestServiceWithRunner(t, runner, func(ctx context.Context, target delivery.Target, outputs []delivery.Output) (delivery.Receipt, error) {
		if len(outputs) > 0 {
			sent = append(sent, target.Platform+":"+outputs[0].Text)
		}
		return delivery.Receipt{}, nil
	})
	defer cleanup()
	var queued QueuedLLMEvent
	service.SetLLMEnqueuer(func(ctx context.Context, event QueuedLLMEvent) error {
		queued = event
		return nil
	})

	req := testRequest(ModeLLM)
	req.Targets = []Target{{Platform: "cli"}}
	if _, err := service.Handle(context.Background(), "secret", req); err != nil {
		t.Fatalf("Handle llm: %v", err)
	}
	if err := service.RunLLMEvent(context.Background(), queued.Event, queued.EventID); err != nil {
		t.Fatalf("RunLLMEvent: %v", err)
	}
	if len(sent) != 1 || sent[0] != "cli:创建失败：工具权限不足" {
		t.Fatalf("sent = %#v", sent)
	}
}

func TestResolveTargetsAllUsesEnabledPlatforms(t *testing.T) {
	service, cleanup := newTestService(t, nil)
	defer cleanup()

	req := testRequest(ModeDirect)
	req.Targets = []Target{{Platform: "all"}}
	resolved, err := service.resolveTargets(req)
	if err != nil {
		t.Fatalf("resolveTargets: %v", err)
	}
	if got := targetSummary(resolved); got != "cli,qqonebot" {
		t.Fatalf("targets = %q", got)
	}
}

func TestHandleRejectsMissingTargets(t *testing.T) {
	service, cleanup := newTestService(t, nil)
	defer cleanup()

	req := testRequest(ModeDirect)
	req.Targets = nil
	resp, err := service.Handle(context.Background(), "secret", req)
	if err == nil {
		t.Fatal("expected missing targets error")
	}
	if resp.Accepted || !strings.Contains(resp.Error, "targets is required") {
		t.Fatalf("response = %#v", resp)
	}
}

func TestHandleRejectsInvalidAllTarget(t *testing.T) {
	service, cleanup := newTestService(t, nil)
	defer cleanup()

	req := testRequest(ModeDirect)
	req.Targets = []Target{{Platform: "all", Type: "group", ID: "123"}}
	resp, err := service.Handle(context.Background(), "secret", req)
	if err == nil {
		t.Fatal("expected invalid all target error")
	}
	if resp.Accepted || !strings.Contains(resp.Error, "platform all") {
		t.Fatalf("response = %#v", resp)
	}
}

func TestResolveTargetsDisabledPlatformBlocksAllTargetTypes(t *testing.T) {
	service, cleanup := newTestService(t, nil)
	defer cleanup()
	service.cfg.DeliveryDisabled.Targets = []config.AtrinisTargetConfig{{Platform: "telegram"}}

	req := testRequest(ModeLLM)
	req.Targets = []Target{{Platform: "telegram"}, {Platform: "telegram", Type: "private", ID: "1"}, {Platform: "telegram", Type: "group", ID: "2"}, {Platform: "cli"}}
	resolved, err := service.resolveTargets(req)
	if err != nil {
		t.Fatalf("resolveTargets: %v", err)
	}
	if got := targetSummary(resolved); got != "cli" {
		t.Fatalf("targets = %q", got)
	}
}

func TestResolveTargetsDisabledAtriwispPrivateChat(t *testing.T) {
	service, cleanup := newTestService(t, nil)
	defer cleanup()
	policy := service.cfg.Atriwisps["watcher"]
	policy.DisabledTargets = []config.AtrinisTargetConfig{{Platform: "telegram", Type: "private", ID: "1"}}
	service.cfg.Atriwisps["watcher"] = policy

	req := testRequest(ModeLLM)
	req.Targets = []Target{{Platform: "telegram", Type: "private", ID: "1"}, {Platform: "telegram", Type: "group", ID: "2"}}
	resolved, err := service.resolveTargets(req)
	if err != nil {
		t.Fatalf("resolveTargets: %v", err)
	}
	if got := targetSummary(resolved); got != "telegram:group:2" {
		t.Fatalf("targets = %q", got)
	}
}

func TestHandleDirectSendsToPrivateAndGroupTargets(t *testing.T) {
	sent := []string{}
	service, cleanup := newTestService(t, func(ctx context.Context, target delivery.Target, outputs []delivery.Output) (delivery.Receipt, error) {
		if len(outputs) > 0 {
			sent = append(sent, target.Platform+":"+target.PrivateUserID+":"+target.GroupID+":"+outputs[0].Text)
		}
		return delivery.Receipt{}, nil
	})
	defer cleanup()

	req := testRequest(ModeDirect)
	req.Title = "警告"
	req.Content = "服务器异常"
	req.Targets = []Target{{Platform: "telegram", Type: "private", ID: "123"}, {Platform: "qqonebot", Type: "group", ID: "456"}}
	resp, err := service.Handle(context.Background(), "secret", req)
	if err != nil {
		t.Fatalf("Handle direct: %v", err)
	}
	if resp.Status != StatusCompleted {
		t.Fatalf("response = %#v", resp)
	}
	want := []string{"qqonebot::456:警告\n服务器异常", "telegram:123::警告\n服务器异常"}
	if strings.Join(sent, "|") != strings.Join(want, "|") {
		t.Fatalf("sent = %#v", sent)
	}
}

func TestHandleDirectSendsToResolvedSuperadminTargets(t *testing.T) {
	sent := []string{}
	service, cleanup := newTestService(t, func(ctx context.Context, target delivery.Target, outputs []delivery.Output) (delivery.Receipt, error) {
		if len(outputs) > 0 {
			sent = append(sent, target.Platform+":"+outputs[0].Text)
		}
		return delivery.Receipt{}, nil
	})
	defer cleanup()

	req := testRequest(ModeDirect)
	req.Title = "警告"
	req.Content = "服务器异常"
	req.Targets = []Target{{Platform: "cli"}, {Platform: "qqofficial"}}
	resp, err := service.Handle(context.Background(), "secret", req)
	if err != nil {
		t.Fatalf("Handle direct: %v", err)
	}
	if resp.Status != StatusCompleted {
		t.Fatalf("response = %#v", resp)
	}
	want := []string{"cli:警告\n服务器异常", "qqofficial:警告\n服务器异常"}
	if strings.Join(sent, "|") != strings.Join(want, "|") {
		t.Fatalf("sent = %#v", sent)
	}
}

func TestHandleDirectCallsOnlyDoesNotSendOutput(t *testing.T) {
	sent := 0
	service, cleanup := newTestService(t, func(ctx context.Context, target delivery.Target, outputs []delivery.Output) (delivery.Receipt, error) {
		sent++
		return delivery.Receipt{}, nil
	})
	defer cleanup()
	caller := &fakePlatformCallerResolver{callers: map[string]*fakePlatformCaller{"qqonebot": {}}}
	service.platformCallers = caller

	req := testRequest(ModeDirect)
	req.Content = ""
	req.Title = ""
	req.Calls = []Call{{Kind: atriena.CallKindCapability, Name: atriena.CapabilityMessageRecall, Platform: "qqonebot", Params: map[string]any{"message_id": 42}}}
	resp, err := service.Handle(context.Background(), "secret", req)
	if err != nil {
		t.Fatalf("Handle direct calls-only: %v", err)
	}
	if resp.Status != StatusCompleted {
		t.Fatalf("response = %#v", resp)
	}
	if sent != 0 {
		t.Fatalf("sent outputs = %d, want 0", sent)
	}
	calls := caller.callers["qqonebot"].calls
	if len(calls) != 1 || calls[0].api != "delete_msg" || calls[0].params["message_id"] != int64(42) {
		t.Fatalf("calls = %#v", calls)
	}
}

func newTestService(t *testing.T, send SenderFunc) (*Service, func()) {
	return newTestServiceWithRunner(t, nil, send)
}

func newTestServiceWithRunner(t *testing.T, runner background.Runner, send SenderFunc) (*Service, func()) {
	t.Helper()
	store, err := sqlite.New(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("sqlite.New: %v", err)
	}
	if send == nil {
		send = func(ctx context.Context, target delivery.Target, outputs []delivery.Output) (delivery.Receipt, error) {
			return delivery.Receipt{}, nil
		}
	}
	service, err := NewService(Options{
		Config: config.AtrinisConfig{
			Enabled:      true,
			AllowedTools: []string{"web_search"},
			Atriwisps: map[string]config.AtrinisAtriwispConfig{
				"watcher":    {AllowedTokens: []string{"home"}, AllowedTools: []string{"shell"}, DisabledExternalTools: []string{"danger_tool"}},
				"configured": {},
				"enabled":    {Enabled: boolPtr(true)},
				"disabled":   {Enabled: boolPtr(false)},
			},
		},
		SandboxRoot:      filepath.Join(t.TempDir(), "sandbox"),
		Tokens:           map[string]string{"home": "secret", "alt": "alt-secret"},
		Store:            store,
		Send:             send,
		Runner:           runner,
		EnabledPlatforms: []string{"cli", "qqonebot"},
		ResolveModel: func(slot string) config.ModelSelection {
			return config.ModelSelection{Provider: "provider-" + slot, Model: "model-" + slot}
		},
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return service, func() { _ = store.Close() }
}

func targetSummary(targets []Target) string {
	parts := make([]string, 0, len(targets))
	for _, target := range targets {
		parts = append(parts, strings.Trim(target.Platform+":"+target.Type+":"+target.ID, ":"))
	}
	return strings.Join(parts, ",")
}

func boolPtr(value bool) *bool {
	return &value
}

func testExternalTool(name string) toolrun.AtriwispToolDeclaration {
	return toolrun.AtriwispToolDeclaration{
		Name:           name,
		Description:    "test external tool",
		Endpoint:       "http://127.0.0.1:32171/tools/" + name,
		TimeoutSeconds: 5,
		Schema:         map[string]any{"type": "object", "properties": map[string]any{"detail": map[string]any{"type": "boolean"}}},
	}
}

type fakePlatformCall struct {
	api    string
	params map[string]any
}

type fakePlatformCaller struct {
	calls []fakePlatformCall
}

func (c *fakePlatformCaller) CallPlatformAPI(ctx context.Context, api string, params map[string]any) (json.RawMessage, error) {
	c.calls = append(c.calls, fakePlatformCall{api: api, params: params})
	return json.RawMessage(`{}`), nil
}

type fakePlatformCallerResolver struct {
	callers map[string]*fakePlatformCaller
}

func (r *fakePlatformCallerResolver) PlatformCaller(platform string) (atriena.PlatformAPICaller, bool) {
	caller, ok := r.callers[platform]
	return caller, ok
}

type fakeBackgroundRunner struct {
	text     string
	requests []background.RunRequest
}

type atrinisRepositoryStore struct {
	storage.Store
	repo storage.AtrinisEventRepository
}

func (s atrinisRepositoryStore) AtrinisEvents() storage.AtrinisEventRepository {
	return s.repo
}

type failOnceReceiptRepository struct {
	storage.AtrinisEventRepository
	failed bool
}

func (r *failOnceReceiptRepository) MarkReportDeliveryDelivered(ctx context.Context, deliveryID, receipt string) error {
	if !r.failed {
		r.failed = true
		return errors.New("receipt persistence failed")
	}
	return r.AtrinisEventRepository.MarkReportDeliveryDelivered(ctx, deliveryID, receipt)
}

func (r *fakeBackgroundRunner) RunBackground(ctx context.Context, req background.RunRequest) (background.RunResult, error) {
	r.requests = append(r.requests, req)
	return background.RunResult{SessionID: "bg-session", MessageID: "bg-message", Text: r.text}, nil
}

func testRequest(mode string) Request {
	return Request{
		Version:  "atriena.v2",
		Atriwisp: Atriwisp{Name: "watcher", Tags: []string{"test"}},
		Source:   "source",
		ID:       "event-1",
		Mode:     mode,
		Format:   "text",
		Content:  "hello",
		Targets:  []Target{{Platform: "cli"}},
	}
}
