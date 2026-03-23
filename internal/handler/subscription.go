package handler

import (
	"log/slog"
	"net/http"

	"subs-aggregator/internal/service"
)

type SubscriptionHandler struct {
	subService *service.SubscriptionService
	logger     *slog.Logger
}

func NewSubscriptionHandler(subService *service.SubscriptionService, logger *slog.Logger) *SubscriptionHandler {
	return &SubscriptionHandler{
		subService: subService,
		logger:     logger,
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
	w.Write([]byte(merged))
}
