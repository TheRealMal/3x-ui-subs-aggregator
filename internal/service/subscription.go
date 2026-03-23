package service

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/url"
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
// If profileTitle is non-empty, it is appended as a fragment for Shadowrocket compatibility.
func BuildSubURL(host, name, profileTitle string) string {
	subURL := fmt.Sprintf("%s/sub/%s", host, HashName(name))
	if profileTitle != "" {
		subURL += "#" + url.PathEscape(profileTitle)
	}
	return subURL
}

type subscriptionResult struct {
	panel string
	data  []byte
	err   error
}

// MergeSubscriptions fetches base64-encoded URI subscriptions from all panels
// concurrently, decodes them, merges the URI lines, and re-encodes as base64.
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

	var allURIs []string
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

		decoded, err := decodeBase64(string(res.data))
		if err != nil {
			s.logger.Warn("failed to decode base64 subscription",
				"panel", res.panel,
				"error", err,
			)
			failCount++
			continue
		}

		for _, line := range strings.Split(decoded, "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				allURIs = append(allURIs, line)
			}
		}
		successCount++
	}

	if successCount == 0 {
		return nil, fmt.Errorf("failed to fetch subscription from all panels")
	}

	s.logger.Info("merged subscriptions",
		"successPanels", successCount,
		"failedPanels", failCount,
		"totalURIs", len(allURIs),
	)

	merged := strings.Join(allURIs, "\n")
	encoded := base64.StdEncoding.EncodeToString([]byte(merged))
	return []byte(encoded), nil
}

// decodeBase64 decodes a base64 string, handling both padded and unpadded input.
func decodeBase64(s string) (string, error) {
	s = strings.TrimSpace(s)
	decoded, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		decoded, err = base64.RawStdEncoding.DecodeString(strings.TrimRight(s, "="))
		if err != nil {
			return "", fmt.Errorf("invalid base64: %w", err)
		}
	}
	return string(decoded), nil
}
