package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"3x-ui-sub-unifier/internal/service"
)

type AdminHandler struct {
	subService    *service.SubscriptionService
	clientService *service.ClientService
	logger        *slog.Logger
}

func NewAdminHandler(subService *service.SubscriptionService, clientService *service.ClientService, logger *slog.Logger) *AdminHandler {
	return &AdminHandler{
		subService:    subService,
		clientService: clientService,
		logger:        logger,
	}
}

func (h *AdminHandler) HandleGetSubURL(w http.ResponseWriter, r *http.Request) {
	email := strings.TrimPrefix(r.URL.Path, "/admin/sub-url/")
	if email == "" {
		writeError(w, http.StatusBadRequest, "missing email")
		return
	}

	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	host := scheme + "://" + r.Host

	subURL, err := h.subService.BuildSubURL(r.Context(), host, email)
	if err != nil {
		h.logger.Error("failed to build sub URL", "email", email, "error", err)
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	writeSuccess(w, map[string]string{"url": subURL})
}

func (h *AdminHandler) HandleCreateClient(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req service.CreateClientRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Email == "" {
		writeError(w, http.StatusBadRequest, "email is required")
		return
	}
	if len(req.InboundIDs) == 0 {
		writeError(w, http.StatusBadRequest, "inboundIds is required")
		return
	}

	results, err := h.clientService.CreateClientAcrossPanels(r.Context(), req)
	if err != nil {
		h.logger.Error("failed to create client", "email", req.Email, "error", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeSuccess(w, results)
}
