package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/fujiwarashigure/ATRI-X/internal/delivery"
	"github.com/fujiwarashigure/ATRI-X/internal/hook"
	"github.com/fujiwarashigure/ATRI-X/internal/llm"
	"github.com/fujiwarashigure/ATRI-X/internal/logging"
	"github.com/fujiwarashigure/ATRI-X/internal/platform"
	"github.com/fujiwarashigure/ATRI-X/internal/signal"
	"github.com/fujiwarashigure/ATRI-X/internal/storage"
)

func (a *Agent) SetOutputManager(manager delivery.Manager) {
	a.outputs = manager
}

func (a *Agent) sendOutputs(ctx context.Context, outputs []delivery.Output) error {
	if !a.turnOutputAllowed(ctx) {
		return nil
	}
	manager := a.outputs
	manager.Sender = agentOutputSender{agent: a, ctx: ctx}
	if manager.Logger == nil {
		manager.Logger = a.logger
	}
	receipt, err := manager.SendNoticesWithReceipt(ctx, outputs)
	if err != nil {
		if count := len(receipt.PlatformMessageIDs); count > 0 {
			a.audit("platform_send_partial", "operation", "send_outputs", "platform_message_count", count, "error", err.Error(), "result", logging.ResultFailed)
			if a.logger != nil {
				a.logger.WarnContext(ctx, "output send failed with partial receipt", "error", err.Error(), "platform_message_count", count)
			}
		}
	}
	return err
}

type agentOutputSender struct {
	agent *Agent
	ctx   context.Context
}

func (s agentOutputSender) SendChat(ctx context.Context, outputs []delivery.Output) (delivery.Receipt, error) {
	if s.agent == nil {
		return delivery.Receipt{}, fmt.Errorf("agent output sender is not configured")
	}
	return s.agent.sendPreparedMedia(s.ctx, outputs, func(resolved []delivery.Output) (delivery.Receipt, error) {
		if msg, ok := platform.MessageContextFrom(s.ctx); ok && msg.Sender != nil {
			return msg.Sender.SendChat(s.ctx, resolved)
		}
		if s.agent.platform == nil {
			return delivery.Receipt{}, fmt.Errorf("chat output sender is not configured")
		}
		return s.agent.platform.SendChat(s.ctx, resolved)
	})
}

// noticeDispatch 是一次通知的落点：队列按平台分条，串行键按目标分。
type noticeDispatch struct {
	platform string
	send     func(context.Context, delivery.Notice) (delivery.Receipt, error)
}

// noticeDispatch 解析这次通知该发给谁。解析放在入队之前：平台没配好就不必占队列。
func (a *Agent) noticeDispatch(ctx context.Context, target delivery.Target) (noticeDispatch, error) {
	if target.Empty() {
		if msg, ok := platform.MessageContextFrom(ctx); ok && msg.Sender != nil {
			return noticeDispatch{platform: noticePlatformName(msg.Platform), send: msg.Sender.SendNotice}, nil
		}
	}
	platformName := strings.TrimSpace(target.Platform)
	if platformName == "" {
		if msg, ok := platform.MessageContextFrom(ctx); ok {
			platformName = msg.Platform
		}
	}
	if platformName == "" && a.platform != nil {
		platformName = a.platform.Name()
	}
	if platformName == "" {
		return noticeDispatch{}, fmt.Errorf("notice target platform is not configured")
	}
	sender := a.platformSenders[platformName]
	if sender == nil {
		return noticeDispatch{}, fmt.Errorf("target platform %q is not configured", platformName)
	}
	return noticeDispatch{
		platform: platformName,
		send: func(sendCtx context.Context, notice delivery.Notice) (delivery.Receipt, error) {
			notice.Target.Platform = platformName
			return sender.SendNotice(sendCtx, notice)
		},
	}, nil
}

// noticePlatformName 给"没有平台名的当前会话发送"一条稳定的队列名。
func noticePlatformName(name string) string {
	if strings.TrimSpace(name) == "" {
		return "context"
	}
	return name
}

func (s agentOutputSender) SendNotice(ctx context.Context, notice delivery.Notice) (delivery.Receipt, error) {
	outputs := notice.Outputs
	if s.agent == nil {
		return delivery.Receipt{}, fmt.Errorf("agent output sender is not configured")
	}
	dispatch, err := s.agent.noticeDispatch(s.ctx, notice.Target)
	if err != nil {
		return delivery.Receipt{}, err
	}
	// 媒体解析与真正发送放进同一段串行执行：同一目标的导出、上传与发送不交叉。
	receipt, err := s.agent.noticeQueues.send(s.ctx, dispatch.platform, notice, func(runCtx context.Context) (delivery.Receipt, error) {
		return s.agent.sendPreparedMedia(runCtx, outputs, func(resolved []delivery.Output) (delivery.Receipt, error) {
			notice.Outputs = resolved
			return dispatch.send(runCtx, notice)
		})
	})
	// 队列满是背压信号，不是故障；调用方可能忽略返回值，所以这里留一条可查的记录。
	if errors.Is(err, signal.ErrQueueFull) && s.agent.logger != nil {
		s.agent.logger.WarnContext(s.ctx, "notice queue full", "platform", dispatch.platform, "error", err.Error())
	}
	return receipt, err
}

type contextTextSender struct {
	ctx    context.Context
	sender delivery.ContextSender
}

func (s contextTextSender) SendChat(ctx context.Context, outputs []delivery.Output) (delivery.Receipt, error) {
	return s.sender.SendChat(s.ctx, outputs)
}

func (s contextTextSender) SendNotice(ctx context.Context, notice delivery.Notice) (delivery.Receipt, error) {
	return s.sender.SendNotice(s.ctx, notice)
}

func (a *Agent) sendChat(ctx context.Context, text string) {
	_, _ = a.sendChatWithReceipt(ctx, text)
}

func (a *Agent) sendNotice(ctx context.Context, notice delivery.Notice) error {
	_, err := a.SendNotice(ctx, notice)
	return err
}

func (a *Agent) prepareAssistantOutput(ctx context.Context, point hook.Point, text string) (string, error) {
	event, err := a.runHook(ctx, hook.Event{Point: point, Message: hook.MessagePayload{Role: string(llm.RoleAssistant), Segments: llm.TextSegments(text)}})
	if err != nil {
		return "", err
	}
	return llm.SegmentsTextOnly(event.Message.Segments), nil
}

func (a *Agent) sendChatWithReceipt(ctx context.Context, text string) (delivery.Receipt, error) {
	if !a.turnOutputAllowed(ctx) {
		return delivery.Receipt{}, nil
	}
	if strings.TrimSpace(text) == "" && bufferAssistantOutput(ctx) {
		return delivery.Receipt{}, nil
	}
	preparedText, err := a.prepareAssistantOutput(ctx, hook.PointAgentOutputPrepared, text)
	if err != nil {
		return delivery.Receipt{}, err
	}
	manager := a.outputs

	manager.Sender = agentOutputSender{agent: a, ctx: ctx}
	if manager.Logger == nil {
		manager.Logger = a.logger
	}
	receipt, err := manager.SendChat(ctx, []delivery.Output{delivery.Text(preparedText)})

	if err != nil {
		if a.logger != nil {
			partial := ""
			if len(receipt.PlatformMessageIDs) > 0 {
				partial = "yes"
			}
			a.logger.WarnContext(ctx, "chat send failed", "error", err.Error(), "partial_receipt", partial)
		}
		return receipt.MarkPartialFailure(err), err
	}
	a.notifyHook(ctx, hook.Event{Point: hook.PointPlatformMessageSent, Message: hook.MessagePayload{Role: string(llm.RoleAssistant), Segments: llm.TextSegments(preparedText)}})

	return receipt, nil
}

func bufferAssistantOutput(ctx context.Context) bool {
	msg, ok := platform.MessageContextFrom(ctx)
	return ok && msg.BufferAssistantOutput
}

func (a *Agent) mapSentAssistantMessage(ctx context.Context, sessionID, messageID string, receipt delivery.Receipt) {
	if len(receipt.PlatformMessageIDs) == 0 || a.store == nil || a.store.Messages() == nil {
		return
	}
	scope := a.scope(ctx)
	for _, platformMessageID := range receipt.PlatformMessageIDs {
		platformMessageID = strings.TrimSpace(platformMessageID)
		if platformMessageID == "" {
			continue
		}
		mapping := storage.PlatformMessageMap{
			Platform:          scope.Platform,
			PlatformScopeID:   scope.PlatformScopeID,
			PlatformMessageID: platformMessageID,
			MessageID:         messageID,
			SessionID:         sessionID,
		}
		if err := a.store.Messages().MapPlatformMessage(ctx, mapping); err != nil {
			a.audit("persistence_error", "session_id", sessionID, "operation", "map_platform_message", "platform_message_id", platformMessageID, "error", err.Error(), "result", logging.ResultFailed)
			if a.logger != nil {
				a.logger.WarnContext(ctx, "map platform message failed", "session_id", sessionID, "platform_message_id", platformMessageID, "error", err.Error())
			}
		}
	}
}

func (a *Agent) RegisterPlatformSender(name string, sender delivery.MessageSender) {
	name = strings.TrimSpace(name)
	if name == "" || sender == nil {
		return
	}
	if a.platformSenders == nil {
		a.platformSenders = map[string]delivery.MessageSender{}
	}
	a.platformSenders[name] = sender
}

func (a *Agent) SendNotice(ctx context.Context, notice delivery.Notice) (delivery.Receipt, error) {
	if !a.turnOutputAllowed(ctx) {
		return delivery.Receipt{}, nil
	}
	if !groupRuntimeNoticeBypass(ctx) && a.noticeTargetBlocked(notice.Target) {
		return delivery.Receipt{}, nil
	}
	manager := a.outputs
	manager.Sender = agentOutputSender{agent: a, ctx: ctx}
	if manager.Logger == nil {
		manager.Logger = a.logger
	}
	return manager.SendNotice(ctx, notice)
}
