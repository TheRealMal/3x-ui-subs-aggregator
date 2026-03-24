package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"subs-aggregator/internal/service"
)

type AdminHandler struct {
	clientService *service.ClientService
	logger        *slog.Logger
	adminSecret   string
	profileTitle  string
}

func NewAdminHandler(clientService *service.ClientService, logger *slog.Logger, adminSecret, profileTitle string) *AdminHandler {
	return &AdminHandler{
		clientService: clientService,
		logger:        logger,
		adminSecret:   adminSecret,
		profileTitle:  profileTitle,
	}
}

func (h *AdminHandler) checkSecret(r *http.Request) bool {
	return r.Header.Get("Authorization") == h.adminSecret
}

// HandleGetSubURL godoc
//
//	@Summary		Get subscription URL for client
//	@Description	Generates a subscription URL for the given client name
//	@Tags			admin
//	@Produce		json
//	@Param			name	path		string	true	"Client name"
//	@Success		200		{object}	Response{obj=SubURLResponse}
//	@Failure		400		{object}	Response
//	@Failure		401		{object}	Response
//	@Security		AdminSecret
//	@Router			/admin/sub-url/{name} [get]
func (h *AdminHandler) HandleGetSubURL(w http.ResponseWriter, r *http.Request) {
	if !h.checkSecret(r) {
		writeError(w, http.StatusUnauthorized, "invalid or missing secret")
		return
	}

	name := r.PathValue("name")
	if name == "" {
		writeError(w, http.StatusBadRequest, "missing name")
		return
	}

	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	host := scheme + "://" + r.Host

	subURL := service.BuildSubURL(host, name, h.profileTitle)
	writeSuccess(w, map[string]string{"url": subURL})
}

// HandleListInbounds godoc
//
//	@Summary		List all inbounds
//	@Description	Returns all inbounds from all configured 3X-UI panels
//	@Tags			admin
//	@Produce		json
//	@Success		200		{object}	Response{obj=[]service.PanelInboundsResult}
//	@Failure		401		{object}	Response
//	@Failure		500		{object}	Response
//	@Security		AdminSecret
//	@Router			/admin/inbounds [get]
func (h *AdminHandler) HandleListInbounds(w http.ResponseWriter, r *http.Request) {
	if !h.checkSecret(r) {
		writeError(w, http.StatusUnauthorized, "invalid or missing secret")
		return
	}

	results, err := h.clientService.ListInboundsAcrossPanels(r.Context())
	if err != nil {
		h.logger.Error("failed to list inbounds", "error", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeSuccess(w, results)
}

// HandleListClients godoc
//
//	@Summary		List all clients
//	@Description	Returns all clients across all panels grouped by name
//	@Tags			admin
//	@Produce		json
//	@Success		200		{object}	Response{obj=[]service.ClientListEntry}
//	@Failure		401		{object}	Response
//	@Failure		500		{object}	Response
//	@Security		AdminSecret
//	@Router			/admin/clients [get]
func (h *AdminHandler) HandleListClients(w http.ResponseWriter, r *http.Request) {
	if !h.checkSecret(r) {
		writeError(w, http.StatusUnauthorized, "invalid or missing secret")
		return
	}

	results, err := h.clientService.ListClientsAcrossPanels(r.Context())
	if err != nil {
		h.logger.Error("failed to list clients", "error", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeSuccess(w, results)
}

// HandleCreateClient godoc
//
//	@Summary		Create client across panels
//	@Description	Creates a client with the same credentials across all panels and requested inbounds
//	@Tags			admin
//	@Accept			json
//	@Produce		json
//	@Param			request	body		service.CreateClientRequest	true	"Client creation request"
//	@Success		200		{object}	Response{obj=[]service.CreateClientResult}
//	@Failure		400		{object}	Response
//	@Failure		401		{object}	Response
//	@Failure		500		{object}	Response
//	@Security		AdminSecret
//	@Router			/admin/clients [post]
func (h *AdminHandler) HandleCreateClient(w http.ResponseWriter, r *http.Request) {
	if !h.checkSecret(r) {
		writeError(w, http.StatusUnauthorized, "invalid or missing secret")
		return
	}

	var req service.CreateClientRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	results, err := h.clientService.CreateClientAcrossPanels(r.Context(), req)
	if err != nil {
		h.logger.Error("failed to create client", "name", req.Name, "error", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeSuccess(w, results)
}

// HandleUpdateExpiry godoc
//
//	@Summary		Update client expiry time
//	@Description	Updates expiryTime for all client entries matching the given name across all panels
//	@Tags			admin
//	@Accept			json
//	@Produce		json
//	@Param			name	path		string						true	"Client name"
//	@Param			request	body		service.UpdateExpiryRequest	true	"Expiry update request"
//	@Success		200		{object}	Response{obj=[]service.UpdateClientResult}
//	@Failure		400		{object}	Response
//	@Failure		401		{object}	Response
//	@Failure		500		{object}	Response
//	@Security		AdminSecret
//	@Router			/admin/clients/{name}/expiry [patch]
func (h *AdminHandler) HandleUpdateExpiry(w http.ResponseWriter, r *http.Request) {
	if !h.checkSecret(r) {
		writeError(w, http.StatusUnauthorized, "invalid or missing secret")
		return
	}

	name := r.PathValue("name")
	if name == "" {
		writeError(w, http.StatusBadRequest, "missing name")
		return
	}

	var req service.UpdateExpiryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	results, err := h.clientService.UpdateExpiryAcrossPanels(r.Context(), name, req)
	if err != nil {
		h.logger.Error("failed to update client expiry", "name", name, "error", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeSuccess(w, results)
}

// HandleUpdateIPLimit godoc
//
//	@Summary		Update client IP limit
//	@Description	Updates limitIp for all client entries matching the given name across all panels
//	@Tags			admin
//	@Accept			json
//	@Produce		json
//	@Param			name	path		string						true	"Client name"
//	@Param			request	body		service.UpdateIPLimitRequest	true	"IP limit update request"
//	@Success		200		{object}	Response{obj=[]service.UpdateClientResult}
//	@Failure		400		{object}	Response
//	@Failure		401		{object}	Response
//	@Failure		500		{object}	Response
//	@Security		AdminSecret
//	@Router			/admin/clients/{name}/ip-limit [patch]
func (h *AdminHandler) HandleUpdateIPLimit(w http.ResponseWriter, r *http.Request) {
	if !h.checkSecret(r) {
		writeError(w, http.StatusUnauthorized, "invalid or missing secret")
		return
	}

	name := r.PathValue("name")
	if name == "" {
		writeError(w, http.StatusBadRequest, "missing name")
		return
	}

	var req service.UpdateIPLimitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	results, err := h.clientService.UpdateIPLimitAcrossPanels(r.Context(), name, req)
	if err != nil {
		h.logger.Error("failed to update client IP limit", "name", name, "error", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeSuccess(w, results)
}
