package service

import (
	"context"
	"fmt"
	"log/slog"

	"3x-ui-sub-unifier/internal/xui"

	"github.com/google/uuid"
)

type CreateClientRequest struct {
	Name       string   `json:"name"`
	Inbounds   []string `json:"inbounds"`
	TotalGB    int64    `json:"totalGB"`
	ExpiryTime int64    `json:"expiryTime"`
	LimitIP    int      `json:"limitIp"`
}

type CreateClientResult struct {
	Panel   string `json:"panel"`
	Inbound string `json:"inbound"`
	Email   string `json:"email"`
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

type PanelInbound struct {
	ID       int    `json:"id"`
	Remark   string `json:"remark"`
	Protocol string `json:"protocol"`
	Port     int    `json:"port"`
	Enable   bool   `json:"enable"`
}

type PanelInboundsResult struct {
	Panel    string         `json:"panel"`
	Inbounds []PanelInbound `json:"inbounds"`
	Error    string         `json:"error,omitempty"`
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

// ListInboundsAcrossPanels returns all inbounds from all configured panels.
func (s *ClientService) ListInboundsAcrossPanels(ctx context.Context) ([]PanelInboundsResult, error) {
	var results []PanelInboundsResult

	for _, panelClient := range s.clients {
		inbounds, err := panelClient.ListInbounds(ctx)
		if err != nil {
			s.logger.Warn("failed to list inbounds", "panel", panelClient.PanelName(), "error", err)
			results = append(results, PanelInboundsResult{
				Panel: panelClient.PanelName(),
				Error: fmt.Sprintf("failed to list inbounds: %v", err),
			})
			continue
		}

		panelInbounds := make([]PanelInbound, len(inbounds))
		for i, inb := range inbounds {
			panelInbounds[i] = PanelInbound{
				ID:       inb.ID,
				Remark:   inb.Remark,
				Protocol: inb.Protocol,
				Port:     inb.Port,
				Enable:   inb.Enable,
			}
		}

		results = append(results, PanelInboundsResult{
			Panel:    panelClient.PanelName(),
			Inbounds: panelInbounds,
		})
	}

	return results, nil
}

// CreateClientAcrossPanels creates a client with the same credentials across all panels and requested inbounds.
// Inbounds are matched by remark. If req.Inbounds is empty, all inbounds on each panel are used.
// Emails are suffixed with a counter (name-1, name-2, ...) to satisfy 3X-UI uniqueness constraints.
func (s *ClientService) CreateClientAcrossPanels(ctx context.Context, req CreateClientRequest) ([]CreateClientResult, error) {
	clientUUID := uuid.New().String()
	subId := HashName(req.Name)

	var results []CreateClientResult
	emailCounter := 1

	for _, panelClient := range s.clients {
		inbounds, err := panelClient.ListInbounds(ctx)
		if err != nil {
			s.logger.Warn("failed to list inbounds for panel",
				"panel", panelClient.PanelName(),
				"error", err,
			)
			errMsg := fmt.Sprintf("failed to list inbounds: %v", err)
			if len(req.Inbounds) == 0 {
				results = append(results, CreateClientResult{
					Panel:   panelClient.PanelName(),
					Success: false,
					Error:   errMsg,
				})
			} else {
				for _, remark := range req.Inbounds {
					results = append(results, CreateClientResult{
						Panel:   panelClient.PanelName(),
						Inbound: remark,
						Success: false,
						Error:   errMsg,
					})
				}
			}
			continue
		}

		type inboundInfo struct {
			id       int
			remark   string
			protocol string
		}

		var targets []inboundInfo
		if len(req.Inbounds) == 0 {
			for _, inbound := range inbounds {
				targets = append(targets, inboundInfo{
					id:       inbound.ID,
					remark:   inbound.Remark,
					protocol: inbound.Protocol,
				})
			}
		} else {
			remarkMap := make(map[string]xui.Inbound)
			for _, inbound := range inbounds {
				remarkMap[inbound.Remark] = inbound
			}
			for _, remark := range req.Inbounds {
				inbound, found := remarkMap[remark]
				if !found {
					results = append(results, CreateClientResult{
						Panel:   panelClient.PanelName(),
						Inbound: remark,
						Success: false,
						Error:   fmt.Sprintf("inbound %q not found on panel", remark),
					})
					continue
				}
				targets = append(targets, inboundInfo{
					id:       inbound.ID,
					remark:   inbound.Remark,
					protocol: inbound.Protocol,
				})
			}
		}

		for _, target := range targets {
			email := fmt.Sprintf("%s-%d", req.Name, emailCounter)
			newClient := xui.Client{
				Email:      email,
				Enable:     true,
				SubId:      subId,
				TotalGB:    req.TotalGB,
				ExpiryTime: req.ExpiryTime,
				LimitIP:    req.LimitIP,
			}
			emailCounter++

			switch target.protocol {
			case "vmess", "vless":
				newClient.ID = clientUUID
			case "trojan", "shadowsocks":
				newClient.Password = clientUUID
			}

			if err := panelClient.AddClient(ctx, target.id, newClient); err != nil {
				s.logger.Warn("failed to add client",
					"panel", panelClient.PanelName(),
					"inbound", target.remark,
					"email", email,
					"error", err,
				)
				results = append(results, CreateClientResult{
					Panel:   panelClient.PanelName(),
					Inbound: target.remark,
					Email:   email,
					Success: false,
					Error:   fmt.Sprintf("failed to add client: %v", err),
				})
				continue
			}

			s.logger.Info("client added successfully",
				"panel", panelClient.PanelName(),
				"inbound", target.remark,
				"email", email,
			)
			results = append(results, CreateClientResult{
				Panel:   panelClient.PanelName(),
				Inbound: target.remark,
				Email:   email,
				Success: true,
			})
		}
	}

	return results, nil
}
