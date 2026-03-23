package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"3x-ui-sub-unifier/internal/service"
)

type AdminHandler struct {
	clientService *service.ClientService
	logger        *slog.Logger
	adminSecret   string
}

func NewAdminHandler(clientService *service.ClientService, logger *slog.Logger, adminSecret string) *AdminHandler {
	return &AdminHandler{
		clientService: clientService,
		logger:        logger,
		adminSecret:   adminSecret,
	}
}

func (h *AdminHandler) checkSecret(r *http.Request) bool {
	return r.URL.Query().Get("secret") == h.adminSecret
}

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

	subURL := service.BuildSubURL(host, name)
	writeSuccess(w, map[string]string{"url": subURL})
}

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
	if len(req.InboundIDs) == 0 {
		writeError(w, http.StatusBadRequest, "inboundIds is required")
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
