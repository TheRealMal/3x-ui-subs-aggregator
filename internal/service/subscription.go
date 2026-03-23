package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"3x-ui-sub-unifier/internal/xui"
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

// FindSubIDByEmail searches all panels for a client with the given email and returns its subscription ID.
func (s *SubscriptionService) FindSubIDByEmail(ctx context.Context, email string) (string, error) {
	for _, panelClient := range s.clients {
		inbounds, err := panelClient.ListInbounds(ctx)
		if err != nil {
			s.logger.Warn("failed to list inbounds",
				"panel", panelClient.PanelName(),
				"error", err,
			)
			continue
		}

		for _, inbound := range inbounds {
			var settings xui.InboundSettings
			if err := json.Unmarshal([]byte(inbound.Settings), &settings); err != nil {
				s.logger.Warn("failed to unmarshal inbound settings",
					"panel", panelClient.PanelName(),
					"inboundID", inbound.ID,
					"error", err,
				)
				continue
			}

			for _, client := range settings.Clients {
				if client.Email == email {
					return client.SubId, nil
				}
			}
		}
	}

	return "", fmt.Errorf("client with email %q not found", email)
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
		go func(idx int, pc *xui.APIClient) {
			defer wg.Done()
			data, err := pc.FetchSubscription(ctx, subId)
			results[idx] = subscriptionResult{
				panel: pc.PanelName(),
				data:  data,
				err:   err,
			}
		}(i, panelClient)
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

// BuildSubURL finds the subscription ID for the given email and returns a subscription URL.
func (s *SubscriptionService) BuildSubURL(ctx context.Context, host string, email string) (string, error) {
	subId, err := s.FindSubIDByEmail(ctx, email)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("%s/sub/%s", host, subId), nil
}
