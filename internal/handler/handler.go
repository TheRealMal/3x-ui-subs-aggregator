package handler

import (
	"encoding/json"
	"net/http"
)

// Response is the standard JSON response envelope.
type Response struct {
	Success bool   `json:"success"`
	Msg     string `json:"msg,omitempty"`
	Obj     any    `json:"obj,omitempty"`
}

// SubURLResponse is the response body for the get-sub-url endpoint.
type SubURLResponse struct {
	URL string `json:"url"`
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func writeSuccess(w http.ResponseWriter, obj any) {
	writeJSON(w, http.StatusOK, Response{Success: true, Obj: obj})
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, Response{Success: false, Msg: msg})
}
