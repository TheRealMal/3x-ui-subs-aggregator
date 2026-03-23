package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"3x-ui-sub-unifier/internal/xui"

	"github.com/google/uuid"
)

type CreateClientRequest struct {
	Email      string `json:"email"`
	InboundIDs []int  `json:"inboundIds"`
	TotalGB    int64  `json:"totalGB"`
	ExpiryTime int64  `json:"expiryTime"`
	LimitIP    int    `json:"limitIp"`
}

type CreateClientResult struct {
	Panel     string `json:"panel"`
	InboundID int    `json:"inboundId"`
	Success   bool   `json:"success"`
	Error     string `json:"error,omitempty"`
}

type ClientService struct {
	clients []*xui.APIClient
	logger  *slog.Logger
}

func NewClientService(clients []*xui.APIClient, logger *slog.Logger) *ClientService {
	return &ClientService{
		clients: clients,
		logger:  logger,
	}
}

// CreateClientAcrossPanels creates a client with the same credentials across all panels and requested inbounds.
func (s *ClientService) CreateClientAcrossPanels(ctx context.Context, req CreateClientRequest) ([]CreateClientResult, error) {
	clientUUID := uuid.New().String()
	subId := strings.ReplaceAll(uuid.New().String(), "-", "")[:16]

	var results []CreateClientResult

	for _, panelClient := range s.clients {
		inbounds, err := panelClient.ListInbounds(ctx)
		if err != nil {
			s.logger.Warn("failed to list inbounds for panel",
				"panel", panelClient.PanelName(),
				"error", err,
			)
			for _, inboundID := range req.InboundIDs {
				results = append(results, CreateClientResult{
					Panel:     panelClient.PanelName(),
					InboundID: inboundID,
					Success:   false,
					Error:     fmt.Sprintf("failed to list inbounds: %v", err),
				})
			}
			continue
		}

		inboundProtocols := make(map[int]string)
		for _, inbound := range inbounds {
			inboundProtocols[inbound.ID] = inbound.Protocol
		}

		for _, inboundID := range req.InboundIDs {
			protocol, found := inboundProtocols[inboundID]
			if !found {
				results = append(results, CreateClientResult{
					Panel:     panelClient.PanelName(),
					InboundID: inboundID,
					Success:   false,
					Error:     fmt.Sprintf("inbound %d not found on panel", inboundID),
				})
				continue
			}

			newClient := xui.Client{
				Email:      req.Email,
				Enable:     true,
				SubId:      subId,
				TotalGB:    req.TotalGB,
				ExpiryTime: req.ExpiryTime,
				LimitIP:    req.LimitIP,
			}

			switch protocol {
			case "vmess", "vless":
				newClient.ID = clientUUID
			case "trojan", "shadowsocks":
				newClient.Password = clientUUID
			}

			if err := panelClient.AddClient(ctx, inboundID, newClient); err != nil {
				s.logger.Warn("failed to add client",
					"panel", panelClient.PanelName(),
					"inboundID", inboundID,
					"email", req.Email,
					"error", err,
				)
				results = append(results, CreateClientResult{
					Panel:     panelClient.PanelName(),
					InboundID: inboundID,
					Success:   false,
					Error:     fmt.Sprintf("failed to add client: %v", err),
				})
				continue
			}

			s.logger.Info("client added successfully",
				"panel", panelClient.PanelName(),
				"inboundID", inboundID,
				"email", req.Email,
			)
			results = append(results, CreateClientResult{
				Panel:     panelClient.PanelName(),
				InboundID: inboundID,
				Success:   true,
			})
		}
	}

	return results, nil
}
