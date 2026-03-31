package service

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"strconv"
	"strings"

	"subs-aggregator/internal/xui"
)

// fallbackMapping holds the per-panel mapping from local dest port to
// the master inbound's external port and the fallback path.
type fallbackMapping struct {
	masterPort int            // The master inbound's external port (e.g. 443).
	portToPath map[int]string // Secondary dest port -> fallback path, e.g. {10001: "/de-access"}.
}

// buildFallbackMapping scans a panel's inbounds for one that has VLESS fallback
// rules configured. Returns nil if no fallbacks are found.
func buildFallbackMapping(inbounds []xui.Inbound, logger *slog.Logger) *fallbackMapping {
	for _, inb := range inbounds {
		var settings xui.InboundSettings
		if err := json.Unmarshal([]byte(inb.Settings), &settings); err != nil {
			continue
		}
		if len(settings.Fallbacks) == 0 {
			continue
		}

		m := &fallbackMapping{
			masterPort: inb.Port,
			portToPath: make(map[int]string),
		}
		for _, fb := range settings.Fallbacks {
			if fb.Path == "" {
				continue // default fallback (e.g. dest:80), skip
			}
			port, ok := fb.DestPort()
			if !ok {
				logger.Warn("could not parse fallback dest port",
					"inbound", inb.Remark,
					"dest", string(fb.Dest),
				)
				continue
			}
			m.portToPath[port] = fb.Path
		}
		if len(m.portToPath) == 0 {
			continue // had fallbacks but none with paths
		}
		return m
	}
	return nil
}

// rewriteURIs rewrites subscription URIs so that secondary inbound ports
// are replaced with the master port and the fallback path is injected.
// If mapping is nil, URIs are returned unchanged.
func rewriteURIs(uris []string, mapping *fallbackMapping) []string {
	if mapping == nil {
		return uris
	}
	out := make([]string, len(uris))
	for i, uri := range uris {
		out[i] = rewriteURI(uri, mapping)
	}
	return out
}

func rewriteURI(uri string, mapping *fallbackMapping) string {
	switch {
	case strings.HasPrefix(uri, "vless://"):
		return rewriteStandardURI(uri, mapping)
	case strings.HasPrefix(uri, "trojan://"):
		return rewriteStandardURI(uri, mapping)
	case strings.HasPrefix(uri, "vmess://"):
		return rewriteVmessURI(uri, mapping)
	case strings.HasPrefix(uri, "ss://"):
		return rewriteShadowsocksURI(uri, mapping)
	default:
		return uri
	}
}

// rewriteStandardURI handles vless:// and trojan:// URIs which share
// the format: scheme://creds@host:PORT?params#fragment
func rewriteStandardURI(uri string, mapping *fallbackMapping) string {
	u, err := url.Parse(uri)
	if err != nil {
		return uri
	}

	host, portStr, err := net.SplitHostPort(u.Host)
	if err != nil {
		return uri
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return uri
	}

	path, ok := mapping.portToPath[port]
	if !ok {
		return uri
	}

	u.Host = net.JoinHostPort(host, strconv.Itoa(mapping.masterPort))
	q := u.Query()
	q.Set("path", path)
	u.RawQuery = q.Encode()
	return u.String()
}

// rewriteVmessURI handles vmess:// URIs where the payload is base64-encoded JSON.
func rewriteVmessURI(uri string, mapping *fallbackMapping) string {
	encoded := strings.TrimPrefix(uri, "vmess://")

	decoded, err := decodeBase64Bytes(encoded)
	if err != nil {
		return uri
	}

	var obj map[string]any
	if err := json.Unmarshal(decoded, &obj); err != nil {
		return uri
	}

	port, ok := jsonPort(obj["port"])
	if !ok {
		return uri
	}

	path, ok := mapping.portToPath[port]
	if !ok {
		return uri
	}

	// Preserve the original type (int vs string) for the port field.
	switch obj["port"].(type) {
	case string:
		obj["port"] = strconv.Itoa(mapping.masterPort)
	default:
		obj["port"] = mapping.masterPort
	}
	obj["path"] = path

	data, err := json.Marshal(obj)
	if err != nil {
		return uri
	}
	return "vmess://" + base64.StdEncoding.EncodeToString(data)
}

// rewriteShadowsocksURI handles SIP002 format: ss://base64(method:password)@host:PORT#remark
func rewriteShadowsocksURI(uri string, mapping *fallbackMapping) string {
	u, err := url.Parse(uri)
	if err != nil {
		return uri
	}

	host, portStr, err := net.SplitHostPort(u.Host)
	if err != nil {
		return uri
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return uri
	}

	path, ok := mapping.portToPath[port]
	if !ok {
		return uri
	}

	u.Host = net.JoinHostPort(host, strconv.Itoa(mapping.masterPort))
	q := u.Query()
	q.Set("path", path)
	u.RawQuery = q.Encode()
	return u.String()
}

// jsonPort extracts a port number from a vmess JSON field which may be
// a float64 (JSON number) or a string.
func jsonPort(v any) (int, bool) {
	switch p := v.(type) {
	case float64:
		return int(p), true
	case string:
		n, err := strconv.Atoi(p)
		return n, err == nil
	default:
		return 0, false
	}
}

// decodeBase64Bytes decodes a base64 string, handling both padded and unpadded input.
func decodeBase64Bytes(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	decoded, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		decoded, err = base64.RawStdEncoding.DecodeString(strings.TrimRight(s, "="))
		if err != nil {
			return nil, fmt.Errorf("invalid base64: %w", err)
		}
	}
	return decoded, nil
}
