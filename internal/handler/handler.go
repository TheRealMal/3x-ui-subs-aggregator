package handler

import (
	"encoding/json"
	"net/http"
)

type response struct {
	Success bool   `json:"success"`
	Msg     string `json:"msg,omitempty"`
	Obj     any    `json:"obj,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func writeSuccess(w http.ResponseWriter, obj any) {
	writeJSON(w, http.StatusOK, response{Success: true, Obj: obj})
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, response{Success: false, Msg: msg})
}
