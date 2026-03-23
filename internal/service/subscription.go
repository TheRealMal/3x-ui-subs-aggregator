package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"

	"subs-aggregator/internal/xui"
)

type SubscriptionService struct {
	clients []*xui.APIClient
	logger  *slog.Logger
}

func NewSubscriptionService(clients []*xui.APIClient, logger *slog.Logger) *SubscriptionService {
	return &SubscriptionService{
		clients: clients,
		logger:  logger,
	}
}

// HashName computes the SHA256 hash of a name, used as the deterministic subscription ID.
func HashName(name string) string {
	h := sha256.Sum256([]byte(name))
	return hex.EncodeToString(h[:])
}

// BuildSubURL computes the subscription URL for the given client name.
func BuildSubURL(host, name string) string {
	return fmt.Sprintf("%s/sub/%s", host, HashName(name))
}

type subscriptionResult struct {
	panel string
	data  []byte
	err   error
}

// xrayConfig is used to extract outbounds from a 3X-UI JSON subscription response.
type xrayConfig struct {
	Outbounds []json.RawMessage `json:"outbounds"`
}

// parseOutbounds extracts outbounds from a panel response that is either
// {"outbounds": [...]} or a raw JSON array [...].
func parseOutbounds(data []byte) ([]json.RawMessage, error) {
	var cfg xrayConfig
	if err := json.Unmarshal(data, &cfg); err == nil && len(cfg.Outbounds) > 0 {
		return cfg.Outbounds, nil
	}

	var arr []json.RawMessage
	if err := json.Unmarshal(data, &arr); err != nil {
		return nil, fmt.Errorf("expected JSON object with outbounds or JSON array: %w", err)
	}

	// Array of full xray configs — extract outbounds from each element.
	var outbounds []json.RawMessage
	for _, item := range arr {
		var nested xrayConfig
		if err := json.Unmarshal(item, &nested); err == nil && len(nested.Outbounds) > 0 {
			outbounds = append(outbounds, nested.Outbounds...)
		}
	}
	if len(outbounds) > 0 {
		return outbounds, nil
	}

	// Plain array of outbound objects.
	return arr, nil
}

// MergeSubscriptions fetches JSON subscriptions from all panels concurrently and merges the outbounds.
func (s *SubscriptionService) MergeSubscriptions(ctx context.Context, subId string) ([]byte, error) {
	results := make([]subscriptionResult, len(s.clients))
	var wg sync.WaitGroup

	for i, panelClient := range s.clients {
		wg.Add(1)
		go func() {
			defer wg.Done()
			data, err := panelClient.FetchSubscription(ctx, subId)
			results[i] = subscriptionResult{
				panel: panelClient.PanelName(),
				data:  data,
				err:   err,
			}
		}()
	}

	wg.Wait()

	var allOutbounds []json.RawMessage
	successCount := 0
	failCount := 0

	for _, res := range results {
		if res.err != nil {
			s.logger.Warn("failed to fetch subscription",
				"panel", res.panel,
				"error", res.err,
			)
			failCount++
			continue
		}

		outbounds, err := parseOutbounds(res.data)
		if err != nil {
			s.logger.Warn("failed to parse JSON subscription",
				"panel", res.panel,
				"error", err,
			)
			failCount++
			continue
		}

		allOutbounds = append(allOutbounds, outbounds...)
		successCount++
	}

	if successCount == 0 {
		return nil, fmt.Errorf("failed to fetch subscription from all panels")
	}

	s.logger.Info("merged subscriptions",
		"successPanels", successCount,
		"failedPanels", failCount,
		"totalOutbounds", len(allOutbounds),
	)

	return json.Marshal(xrayConfig{Outbounds: allOutbounds})
}
