package atrinis

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/fujiwarashigure/ATRI-X/internal/background"
	"github.com/fujiwarashigure/ATRI-X/internal/delivery"
	"github.com/fujiwarashigure/ATRI-X/internal/storage"
	"github.com/fujiwarashigure/ATRI-X/internal/tool"
)

func (s *Service) prepareReport(ctx context.Context, event Event, eventID, resultJSON, sessionID, messageID string, result background.JSONResult) (bool, error) {
	resolved, err := decodeResolvedTargets(event.ResolvedTargets)
	if err != nil {
		return false, err
	}
	if len(resolved) == 0 {
		return false, s.completeEventWithSession(ctx, eventID, event.ResolvedTargets, StatusCompleted, sessionID, resultJSON, "")
	}
	if s.media != nil {
		result.ReportSegments, err = s.importReportSegments(ctx, result.ReportSegments, eventID, filepath.Join(s.sandboxRoot, filepath.FromSlash(atrinisSandboxSubdir(event.Request.Atriwisp.Name))))
		if err != nil {
			return false, err
		}
		encoded, err := json.Marshal(result)
		if err != nil {
			return false, err
		}
		resultJSON = string(encoded)
	}
	outputs, err := background.BuildReportOutputs(result.Report, result.ReportSegments, tool.SandboxContext{
		Dir:            filepath.Join(s.sandboxRoot, filepath.FromSlash(atrinisSandboxSubdir(event.Request.Atriwisp.Name))),
		Background:     true,
		BackgroundKind: tool.BackgroundKindAtrinis,
	})
	if err != nil {
		return false, err
	}
	if len(resolved) == 0 || len(outputs) == 0 {
		return false, s.completeEventWithSession(ctx, eventID, event.ResolvedTargets, StatusCompleted, sessionID, resultJSON, "")
	}

	deliveries := make([]storage.CreateAtrinisReportDeliveryRequest, 0, len(resolved)*len(outputs))
	for _, target := range resolved {
		targetJSON, err := json.Marshal(target)
		if err != nil {
			return false, fmt.Errorf("marshal atrinis report target: %w", err)
		}
		for _, output := range outputs {
			outputJSON, err := json.Marshal(output)
			if err != nil {
				return false, fmt.Errorf("marshal atrinis report output: %w", err)
			}
			deliveries = append(deliveries, storage.CreateAtrinisReportDeliveryRequest{
				Target:    string(targetJSON),
				Output:    string(outputJSON),
				MessageID: messageID,
			})
		}
	}
	if err := s.store.AtrinisEvents().PrepareReport(ctx, storage.PrepareAtrinisReportRequest{
		EventID:           eventID,
		ResolvedTargets:   event.ResolvedTargets,
		SessionID:         sessionID,
		Result:            resultJSON,
		Deliveries:        deliveries,
		ResultReadyStatus: StatusResultReady,
	}); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Service) deliverReport(ctx context.Context, eventID string) error {
	if s.send == nil {
		return fmt.Errorf("atrinis sender is not configured")
	}
	repo := s.store.AtrinisEvents()
	claimed, err := repo.ClaimReport(ctx, eventID, StatusResultReady, StatusDelivering)
	if err != nil {
		return err
	}
	if !claimed {
		return nil
	}
	event, err := repo.Get(ctx, eventID)
	if err != nil {
		return s.releaseReport(ctx, eventID, err)
	}
	deliveries, err := repo.ListReportDeliveries(ctx, eventID)
	if err != nil {
		return s.releaseReport(ctx, eventID, err)
	}
	for index := 0; index < len(deliveries); {
		item := deliveries[index]
		if item.Status == storage.AtrinisReportDeliveryDelivered {
			index++
			continue
		}
		var target Target
		if err := json.Unmarshal([]byte(item.Target), &target); err != nil {
			return s.failReportDelivery(ctx, eventID, item.ID, fmt.Errorf("decode atrinis report target: %w", err))
		}
		batch := []storage.AtrinisReportDelivery{item}
		index++
		for index < len(deliveries) {
			next := deliveries[index]
			if next.Status == storage.AtrinisReportDeliveryDelivered || next.Target != item.Target {
				break
			}
			batch = append(batch, next)
			index++
		}
		outputs := make([]delivery.Output, 0, len(batch))
		for _, deliveryItem := range batch {
			var output delivery.Output
			if err := json.Unmarshal([]byte(deliveryItem.Output), &output); err != nil {
				return s.failReportDelivery(ctx, eventID, deliveryItem.ID, fmt.Errorf("decode atrinis report output: %w", err))
			}
			if err := repo.StartReportDelivery(ctx, deliveryItem.ID); err != nil {
				return s.failReportDelivery(ctx, eventID, deliveryItem.ID, err)
			}
			outputs = append(outputs, output)
		}
		receipt, err := s.send(ctx, target.ToDeliveryTarget(), outputs)
		if err != nil {
			return s.failReportDelivery(ctx, eventID, batch[0].ID, err)
		}
		if err := s.cacheMediaReceipt(ctx, target, outputs, receipt); err != nil {
			return s.failReportDelivery(ctx, eventID, batch[0].ID, err)
		}
		receiptJSON, err := json.Marshal(receipt)
		if err != nil {
			return s.failReportDelivery(ctx, eventID, batch[0].ID, fmt.Errorf("marshal atrinis report receipt: %w", err))
		}
		for _, deliveryItem := range batch {
			if err := repo.MarkReportDeliveryDelivered(ctx, deliveryItem.ID, string(receiptJSON)); err != nil {
				return s.releaseReport(ctx, eventID, err)
			}
		}
		s.mapReportReceipt(ctx, event.EventKey, target, event.SessionID, batch[0].MessageID, receipt)
	}
	if err := repo.CompleteReport(ctx, eventID, StatusDelivering, StatusCompleted); err != nil {
		return s.releaseReport(ctx, eventID, err)
	}
	s.auditEvent("atrinis.llm_completed", "event_id", eventID, "event_key", event.EventKey, "session_id", event.SessionID)
	s.logInfo("atrinis llm completed", "event_id", eventID, "event_key", event.EventKey, "session_id", event.SessionID)
	return nil
}

func (s *Service) failReportDelivery(ctx context.Context, eventID, deliveryID string, deliveryErr error) error {
	if err := s.store.AtrinisEvents().MarkReportDeliveryFailed(ctx, eventID, deliveryID, StatusResultReady, deliveryErr.Error()); err != nil {
		return fmt.Errorf("%v; persist delivery failure: %w", deliveryErr, err)
	}
	return deliveryErr
}

func (s *Service) releaseReport(ctx context.Context, eventID string, deliveryErr error) error {
	if err := s.store.AtrinisEvents().ReleaseReport(ctx, eventID, StatusDelivering, StatusResultReady, deliveryErr.Error()); err != nil {
		return fmt.Errorf("%v; release report claim: %w", deliveryErr, err)
	}
	return deliveryErr
}

func (s *Service) resetDeliveringReports(ctx context.Context) error {
	return s.store.AtrinisEvents().ResetDeliveringReports(ctx, StatusDelivering, StatusResultReady)
}

func (s *Service) recoverReports(ctx context.Context, resetDelivering bool) error {
	repo := s.store.AtrinisEvents()
	if resetDelivering {
		if err := s.resetDeliveringReports(ctx); err != nil {
			return err
		}
	}
	ids, err := repo.ListResultReadyReportIDs(ctx, StatusResultReady)
	if err != nil {
		return err
	}
	var firstErr error
	for _, eventID := range ids {
		if err := s.deliverReport(ctx, eventID); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			s.logWarn("recover atrinis report failed", "event_id", eventID, "error", err.Error())
		}
	}
	return firstErr
}
