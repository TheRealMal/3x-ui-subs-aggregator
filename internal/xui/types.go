package xui

import "encoding/json"

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
	Clients []Client `json:"clients"`
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
