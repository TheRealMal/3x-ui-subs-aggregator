package service

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"
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
	data  string
	err   error
}

// MergeSubscriptions fetches subscriptions from all panels concurrently and merges the results.
func (s *SubscriptionService) MergeSubscriptions(ctx context.Context, subId string) (string, error) {
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

	var allLines []string
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

		trimmed := strings.TrimSpace(res.data)
		decoded, err := base64.StdEncoding.DecodeString(trimmed)
		if err != nil {
			decoded, err = base64.RawStdEncoding.DecodeString(trimmed)
			if err != nil {
				s.logger.Warn("failed to decode subscription data",
					"panel", res.panel,
					"error", err,
				)
				failCount++
				continue
			}
		}

		lines := strings.Split(string(decoded), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line != "" {
				allLines = append(allLines, line)
			}
		}
		successCount++
	}

	if successCount == 0 {
		return "", fmt.Errorf("failed to fetch subscription from all panels")
	}

	s.logger.Info("merged subscriptions",
		"successPanels", successCount,
		"failedPanels", failCount,
		"totalLines", len(allLines),
	)

	merged := strings.Join(allLines, "\n")
	encoded := base64.StdEncoding.EncodeToString([]byte(merged))
	return encoded, nil
}

