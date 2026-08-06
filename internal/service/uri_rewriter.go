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

// fallbackRoute describes how traffic reaches a secondary inbound via the master.
// Exactly one of Path or SNI is set.
type fallbackRoute struct {
	Path string // URL path trigger, e.g. "/de-access".
	SNI  string // SNI-based trigger, e.g. "de.yourdomain.com".
}

// fallbackMapping holds the per-panel mapping from local dest port to
// the master inbound's external port, host, and the route (path or SNI).
type fallbackMapping struct {
	masterHost  string                // Public host extracted from master inbound's URI (e.g. "1.2.3.4").
	masterPort  int                   // The master inbound's external port (e.g. 443).
	portToRoute map[int]fallbackRoute // Secondary dest port -> route info.
}

// buildFallbackMapping scans a panel's inbounds for one that has VLESS fallback
// rules configured. Returns nil if no fallbacks are found.
func buildFallbackMapping(inbounds []xui.Inbound, logger *slog.Logger) *fallbackMapping {
	for _, inb := range inbounds {
		settings, err := inb.ParseSettings()
		if err != nil {
			continue
		}
		if len(settings.Fallbacks) == 0 {
			continue
		}

		m := &fallbackMapping{
			masterPort:  inb.Port,
			portToRoute: make(map[int]fallbackRoute),
		}
		for _, fb := range settings.Fallbacks {
			if fb.Path == "" && fb.Name == "" {
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
			m.portToRoute[port] = fallbackRoute{
				Path: fb.Path,
				SNI:  fb.Name,
			}
		}
		if len(m.portToRoute) == 0 {
			continue // had fallbacks but none with paths or SNI
		}
		return m
	}
	return nil
}

// rewriteURIs rewrites subscription URIs so that secondary inbound ports
// are replaced with the master port/host and the fallback path is injected.
// If mapping is nil, URIs are returned unchanged.
func rewriteURIs(uris []string, mapping *fallbackMapping) []string {
	if mapping == nil {
		return uris
	}
	// First pass: extract the public host from a URI that uses the master port.
	mapping.masterHost = extractMasterHost(uris, mapping.masterPort)

	out := make([]string, len(uris))
	for i, uri := range uris {
		out[i] = rewriteURI(uri, mapping)
	}
	return out
}

// extractMasterHost finds the first URI whose port matches masterPort and
// returns its host (the public address clients should connect to).
func extractMasterHost(uris []string, masterPort int) string {
	for _, uri := range uris {
		host, port := extractHostPort(uri)
		if port == masterPort && host != "" {
			return host
		}
	}
	return ""
}

// extractHostPort returns the host and port from a proxy URI, regardless of protocol.
func extractHostPort(uri string) (string, int) {
	switch {
	case strings.HasPrefix(uri, "vmess://"):
		encoded := strings.TrimPrefix(uri, "vmess://")
		decoded, err := decodeBase64Bytes(encoded)
		if err != nil {
			return "", 0
		}
		var obj map[string]any
		if json.Unmarshal(decoded, &obj) != nil {
			return "", 0
		}
		host, _ := obj["add"].(string)
		port, ok := jsonPort(obj["port"])
		if !ok {
			return "", 0
		}
		return host, port
	default:
		// vless://, trojan://, ss:// all use standard URI format
		u, err := url.Parse(uri)
		if err != nil {
			return "", 0
		}
		host, portStr, err := net.SplitHostPort(u.Host)
		if err != nil {
			return "", 0
		}
		port, err := strconv.Atoi(portStr)
		if err != nil {
			return "", 0
		}
		return host, port
	}
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

	route, ok := mapping.portToRoute[port]
	if !ok {
		return uri
	}

	newHost := host
	if mapping.masterHost != "" {
		newHost = mapping.masterHost
	}
	u.Host = net.JoinHostPort(newHost, strconv.Itoa(mapping.masterPort))
	q := u.Query()
	if route.Path != "" {
		q.Set("path", route.Path)
		q.Set("type", "http")
	}
	if route.SNI != "" {
		q.Set("sni", route.SNI)
		q.Set("host", route.SNI)
	}
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

	route, ok := mapping.portToRoute[port]
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
	if route.Path != "" {
		obj["path"] = route.Path
		obj["net"] = "http"
	}
	if route.SNI != "" {
		obj["sni"] = route.SNI
		obj["host"] = route.SNI
	}
	if mapping.masterHost != "" {
		obj["add"] = mapping.masterHost
	}

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

	route, ok := mapping.portToRoute[port]
	if !ok {
		return uri
	}

	newHost := host
	if mapping.masterHost != "" {
		newHost = mapping.masterHost
	}
	u.Host = net.JoinHostPort(newHost, strconv.Itoa(mapping.masterPort))
	q := u.Query()
	if route.Path != "" {
		q.Set("path", route.Path)
		q.Set("type", "http")
	}
	if route.SNI != "" {
		q.Set("sni", route.SNI)
		q.Set("host", route.SNI)
	}
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
