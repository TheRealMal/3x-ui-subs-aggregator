package xui

import (
	"encoding/json"
	"net"
	"strconv"
)

type APIResponse struct {
	Success bool            `json:"success"`
	Msg     string          `json:"msg"`
	Obj     json.RawMessage `json:"obj"`
}

type Inbound struct {
	ID             int    `json:"id"`
	Remark         string `json:"remark"`
	Protocol       string `json:"protocol"`
	Settings       string `json:"settings"`
	StreamSettings string `json:"streamSettings"`
	Port           int    `json:"port"`
	Enable         bool   `json:"enable"`
}

type InboundSettings struct {
	Clients   []Client   `json:"clients"`
	Fallbacks []Fallback `json:"fallbacks,omitempty"`
}

type Fallback struct {
	Name string          `json:"name,omitempty"`
	Path string          `json:"path,omitempty"`
	Dest json.RawMessage `json:"dest"`
	Xver int             `json:"xver,omitempty"`
}

// DestPort extracts the destination port from the Dest field,
// which can be a JSON number (10001) or string ("10001" or "127.0.0.1:10001").
func (f *Fallback) DestPort() (int, bool) {
	var port int
	if json.Unmarshal(f.Dest, &port) == nil {
		return port, true
	}

	var s string
	if json.Unmarshal(f.Dest, &s) == nil {
		if p, err := strconv.Atoi(s); err == nil {
			return p, true
		}
		if _, portStr, err := net.SplitHostPort(s); err == nil {
			if p, err := strconv.Atoi(portStr); err == nil {
				return p, true
			}
		}
	}

	return 0, false
}

type Client struct {
	ID         string `json:"id,omitempty"`
	Password   string `json:"password,omitempty"`
	Email      string `json:"email"`
	Enable     bool   `json:"enable"`
	SubId      string `json:"subId"`
	Flow       string `json:"flow,omitempty"`
	TotalGB    int64  `json:"totalGB"`
	ExpiryTime int64  `json:"expiryTime"`
	LimitIP    int    `json:"limitIp"`
}

type ClientTraffic struct {
	ID         int    `json:"id"`
	InboundID  int    `json:"inboundId"`
	Enable     bool   `json:"enable"`
	Email      string `json:"email"`
	Up         int64  `json:"up"`
	Down       int64  `json:"down"`
	Total      int64  `json:"total"`
	ExpiryTime int64  `json:"expiryTime"`
}

type AddClientRequest struct {
	ID       int    `json:"id"`
	Settings string `json:"settings"`
}
