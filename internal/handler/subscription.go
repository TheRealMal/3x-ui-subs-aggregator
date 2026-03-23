package handler

import (
	"encoding/base64"
	"log/slog"
	"net/http"

	"subs-aggregator/internal/service"
)

type SubscriptionHandler struct {
	subService   *service.SubscriptionService
	logger       *slog.Logger
	profileTitle string
}

func NewSubscriptionHandler(subService *service.SubscriptionService, logger *slog.Logger, profileTitle string) *SubscriptionHandler {
	return &SubscriptionHandler{
		subService:   subService,
		logger:       logger,
		profileTitle: profileTitle,
	}
}

func (h *SubscriptionHandler) HandleSubscription(w http.ResponseWriter, r *http.Request) {
	subId := r.PathValue("subId")
	if subId == "" {
		http.Error(w, "missing subscription id", http.StatusBadRequest)
		return
	}

	merged, err := h.subService.MergeSubscriptions(r.Context(), subId)
	if err != nil {
		h.logger.Error("failed to merge subscriptions", "subId", subId, "error", err)
		http.Error(w, "subscription not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if h.profileTitle != "" {
		encoded := base64.StdEncoding.EncodeToString([]byte(h.profileTitle))
		w.Header().Set("profile-title", "base64:"+encoded)
	}
	w.Write(merged)
}
