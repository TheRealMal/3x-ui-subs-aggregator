package xui

import (
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"
)

type APIResponse struct {
	Success bool            `json:"success"`
	Msg     string          `json:"msg"`
	Obj     json.RawMessage `json:"obj"`
}

// RawJSON holds a field that 3X-UI encodes differently across panel versions:
// legacy panels return a JSON-encoded *string* (e.g. "{\"clients\":[]}"),
// while v3 panels return the nested object directly. Either way the stored
// bytes are the inner JSON document, so callers can unmarshal it as-is.
type RawJSON []byte

func (r *RawJSON) UnmarshalJSON(data []byte) error {
	// Legacy form: a JSON string whose contents are themselves JSON.
	if len(data) > 0 && data[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		*r = RawJSON(s)
		return nil
	}
	*r = append((*r)[:0], data...)
	return nil
}

func (r RawJSON) MarshalJSON() ([]byte, error) {
	if len(r) == 0 {
		return []byte("null"), nil
	}
	return r, nil
}

// IsEmpty reports whether the field carries no usable JSON document.
func (r RawJSON) IsEmpty() bool {
	s := strings.TrimSpace(string(r))
	return s == "" || s == "null"
}

type Inbound struct {
	ID             int     `json:"id"`
	Remark         string  `json:"remark"`
	Protocol       string  `json:"protocol"`
	Settings       RawJSON `json:"settings"`
	StreamSettings RawJSON `json:"streamSettings"`
	Port           int     `json:"port"`
	Enable         bool    `json:"enable"`
}

// ParseSettings decodes the inbound's settings document. An absent or empty
// settings field yields a zero-value InboundSettings rather than an error.
func (i *Inbound) ParseSettings() (InboundSettings, error) {
	var settings InboundSettings
	if i.Settings.IsEmpty() {
		return settings, nil
	}
	if err := json.Unmarshal(i.Settings, &settings); err != nil {
		return settings, fmt.Errorf("parsing inbound settings: %w", err)
	}
	return settings, nil
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

	// v3 panel fields. Omitted on legacy panels, which ignore them anyway.
	TgID     int64  `json:"tgId,omitempty"`
	Comment  string `json:"comment,omitempty"`
	Group    string `json:"group,omitempty"`
	Security string `json:"security,omitempty"`
	Reset    int    `json:"reset,omitempty"`
	Auth     string `json:"auth,omitempty"`
}

// ClientPatch describes a partial update to a client. Nil fields are left
// untouched, which matters on v3 panels where an update replaces the whole
// stored row rather than merging into it.
type ClientPatch struct {
	ExpiryTime *int64
	LimitIP    *int
	TotalGB    *int64
	Enable     *bool
}

// ApplyTo mutates a Client in place with the fields the patch sets.
func (p ClientPatch) ApplyTo(c *Client) {
	if p.ExpiryTime != nil {
		c.ExpiryTime = *p.ExpiryTime
	}
	if p.LimitIP != nil {
		c.LimitIP = *p.LimitIP
	}
	if p.TotalGB != nil {
		c.TotalGB = *p.TotalGB
	}
	if p.Enable != nil {
		c.Enable = *p.Enable
	}
}

// ApplyToMap writes the patched fields into a raw client payload, using the
// JSON names the panel expects.
func (p ClientPatch) ApplyToMap(m map[string]any) {
	if p.ExpiryTime != nil {
		m["expiryTime"] = *p.ExpiryTime
	}
	if p.LimitIP != nil {
		m["limitIp"] = *p.LimitIP
	}
	if p.TotalGB != nil {
		m["totalGB"] = *p.TotalGB
	}
	if p.Enable != nil {
		m["enable"] = *p.Enable
	}
}

// ClientSecret returns the credential that identifies a client for the given
// protocol: a password for trojan/shadowsocks, the UUID otherwise.
func ClientSecret(protocol string, client Client) string {
	switch protocol {
	case "trojan", "shadowsocks":
		return client.Password
	default:
		return client.ID
	}
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

// AddClientRequest is the legacy (pre-v3) body for inbounds/addClient and
// inbounds/updateClient, where clients are nested inside an inbound's
// settings JSON blob.
type AddClientRequest struct {
	ID       int    `json:"id"`
	Settings string `json:"settings"`
}

// AddClientRequestV3 is the body for panel/api/clients/add, where a client is
// a first-class row attached to one or more inbounds in a single call.
type AddClientRequestV3 struct {
	Client     Client `json:"client"`
	InboundIDs []int  `json:"inboundIds"`
}
