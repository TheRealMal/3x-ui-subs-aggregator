package xui

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"subs-aggregator/internal/config"
)

// APIVersion identifies which generation of the 3X-UI panel API a host speaks.
type APIVersion int

const (
	// APIVersionUnknown means the panel has not been probed yet.
	APIVersionUnknown APIVersion = iota
	// APIVersionLegacy is the pre-3.0 API: clients live inside an inbound's
	// settings blob and unsafe requests need no CSRF token.
	APIVersionLegacy
	// APIVersionV3 is the 3.x API: clients are first-class rows under
	// /panel/api/clients/*, and cookie-authenticated unsafe requests must
	// carry an X-CSRF-Token header.
	APIVersionV3
)

func (v APIVersion) String() string {
	switch v {
	case APIVersionLegacy:
		return "legacy"
	case APIVersionV3:
		return "v3"
	default:
		return "unknown"
	}
}

// safeMethods never carry a CSRF token, mirroring the panel's own middleware.
var safeMethods = map[string]bool{
	http.MethodGet:     true,
	http.MethodHead:    true,
	http.MethodOptions: true,
	http.MethodTrace:   true,
}

type APIClient struct {
	panel      config.PanelConfig
	httpClient *http.Client
	logger     *slog.Logger
	mu         sync.Mutex
	loggedIn   atomic.Bool

	// csrfMu guards csrfToken, which is minted per session and replayed on
	// every unsafe request against a v3 panel.
	csrfMu     sync.Mutex
	csrfToken  string
	apiVersion atomic.Int32
}

func NewAPIClient(panel config.PanelConfig, logger *slog.Logger) *APIClient {
	jar, _ := cookiejar.New(nil)
	return &APIClient{
		panel: panel,
		httpClient: &http.Client{
			Jar:     jar,
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			},
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		logger: logger,
	}
}

func (c *APIClient) PanelName() string {
	return c.panel.Name
}

// APIVersion returns the panel generation detected for this host.
func (c *APIClient) APIVersion() APIVersion {
	return APIVersion(c.apiVersion.Load())
}

// usesBearerAuth reports whether this panel is configured with an API token,
// in which case no session login (and no CSRF dance) is needed at all.
func (c *APIClient) usesBearerAuth() bool {
	return c.panel.APIToken != ""
}

// detectAPIVersion probes GET <basePath>csrf-token, which only exists on v3
// panels. The result is cached for the lifetime of the client.
func (c *APIClient) detectAPIVersion(ctx context.Context) APIVersion {
	if v := c.APIVersion(); v != APIVersionUnknown {
		return v
	}

	version := APIVersionLegacy
	if token, err := c.fetchCSRFToken(ctx); err != nil {
		c.logger.Debug("csrf-token probe failed, assuming legacy panel",
			"panel", c.panel.Name, "error", err)
	} else if token != "" {
		version = APIVersionV3
		c.csrfMu.Lock()
		c.csrfToken = token
		c.csrfMu.Unlock()
	}

	c.apiVersion.Store(int32(version))
	c.logger.Info("detected panel API version", "panel", c.panel.Name, "version", version)
	return version
}

// fetchCSRFToken mints a fresh CSRF token for the current session.
// Returns an empty token (no error) when the endpoint is absent.
func (c *APIClient) fetchCSRFToken(ctx context.Context) (string, error) {
	reqURL := c.apiBaseURL() + c.panel.BasePath + "csrf-token"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return "", fmt.Errorf("creating csrf-token request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("executing csrf-token request: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading csrf-token response: %w", err)
	}
	if resp.StatusCode == http.StatusNotFound {
		return "", nil // legacy panel: endpoint does not exist
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("csrf-token returned status %d", resp.StatusCode)
	}

	var apiResp APIResponse
	if err := json.Unmarshal(data, &apiResp); err != nil {
		return "", fmt.Errorf("decoding csrf-token response: %w", err)
	}
	if !apiResp.Success {
		return "", fmt.Errorf("csrf-token request rejected: %s", apiResp.Msg)
	}

	var token string
	if err := json.Unmarshal(apiResp.Obj, &token); err != nil {
		return "", fmt.Errorf("csrf-token payload is not a string: %w", err)
	}
	return token, nil
}

// csrfHeader returns the cached CSRF token, minting one if needed.
func (c *APIClient) csrfHeader(ctx context.Context) string {
	c.csrfMu.Lock()
	token := c.csrfToken
	c.csrfMu.Unlock()
	if token != "" {
		return token
	}
	return c.refreshCSRFToken(ctx)
}

// refreshCSRFToken discards the cached token and mints a new one. Used after
// login (which rotates the session) and after a 403 rejection.
func (c *APIClient) refreshCSRFToken(ctx context.Context) string {
	token, err := c.fetchCSRFToken(ctx)
	if err != nil {
		c.logger.Debug("failed to refresh CSRF token", "panel", c.panel.Name, "error", err)
		return ""
	}
	c.csrfMu.Lock()
	c.csrfToken = token
	c.csrfMu.Unlock()
	return token
}

func (c *APIClient) clearCSRFToken() {
	c.csrfMu.Lock()
	c.csrfToken = ""
	c.csrfMu.Unlock()
}

func (c *APIClient) apiBaseURL() string {
	return fmt.Sprintf("%s:%d", c.panel.Address, c.panel.APIPort)
}

func (c *APIClient) subBaseURL() string {
	return fmt.Sprintf("%s:%d", c.panel.Address, c.panel.SubPort)
}

func (c *APIClient) Login(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.loggedIn.Load() {
		return nil
	}

	// A Bearer API token authenticates every request on its own — there is no
	// session to establish and CSRF is short-circuited by the panel.
	if c.usesBearerAuth() {
		version := c.detectAPIVersion(ctx)
		// Mark authenticated before probing so the probe does not recurse
		// back into Login through ensureLoggedIn.
		c.loggedIn.Store(true)
		if err := c.verifyAPIToken(ctx); err != nil {
			c.loggedIn.Store(false)
			return err
		}
		c.logger.Info("using API token authentication", "panel", c.panel.Name, "apiVersion", version)
		return nil
	}

	version := c.detectAPIVersion(ctx)

	body, contentType, err := c.buildLoginBody(version)
	if err != nil {
		return err
	}

	reqURL := c.apiBaseURL() + c.panel.BasePath + "login"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("creating login request: %w", err)
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")

	// v3 panels reject unsafe requests without a CSRF token, answering 403
	// with an empty body.
	if version == APIVersionV3 {
		if token := c.csrfHeader(ctx); token != "" {
			req.Header.Set("X-CSRF-Token", token)
		}
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("executing login request: %w", err)
	}
	data, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return fmt.Errorf("reading login response: %w", err)
	}

	// The CSRF token may have expired between minting and use; the panel then
	// answers 403 with no body. Mint a fresh one and retry once.
	if resp.StatusCode == http.StatusForbidden && version == APIVersionV3 {
		c.logger.Debug("login rejected by CSRF check, retrying with a fresh token", "panel", c.panel.Name)
		token := c.refreshCSRFToken(ctx)
		retry, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(body))
		if err != nil {
			return fmt.Errorf("creating login retry request: %w", err)
		}
		retry.Header.Set("Content-Type", contentType)
		retry.Header.Set("Accept", "application/json")
		retry.Header.Set("X-Requested-With", "XMLHttpRequest")
		if token != "" {
			retry.Header.Set("X-CSRF-Token", token)
		}
		resp, err = c.httpClient.Do(retry)
		if err != nil {
			return fmt.Errorf("executing login retry request: %w", err)
		}
		data, err = io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return fmt.Errorf("reading login retry response: %w", err)
		}
	}

	apiResp, err := parseLoginResponse(resp.StatusCode, data)
	if err != nil {
		return err
	}
	if !apiResp.Success {
		return fmt.Errorf("login failed: %s", apiResp.Msg)
	}

	// Logging in rotates the session, invalidating any token minted against
	// the pre-login session.
	c.clearCSRFToken()

	c.loggedIn.Store(true)
	c.logger.Info("logged in to panel", "panel", c.panel.Name, "apiVersion", version)
	return nil
}

// verifyAPIToken makes one cheap authenticated call so an invalid or disabled
// token is reported at startup, the same way a bad password is, instead of
// surfacing later as a subscription fetch failure.
func (c *APIClient) verifyAPIToken(ctx context.Context) error {
	data, statusCode, err := c.executeRequest(ctx, http.MethodGet, "panel/api/inbounds/list", nil)
	if err != nil {
		return fmt.Errorf("verifying API token: %w", err)
	}

	switch statusCode {
	case http.StatusOK:
		var resp APIResponse
		if err := json.Unmarshal(data, &resp); err != nil {
			return fmt.Errorf("verifying API token: unexpected response (body: %q)", preview(data))
		}
		if !resp.Success {
			return fmt.Errorf("API token rejected by panel: %s", resp.Msg)
		}
		return nil
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("API token rejected by panel (HTTP %d): check that the token exists "+
			"and is enabled under Settings -> Security -> API Token", statusCode)
	case http.StatusNotFound:
		return fmt.Errorf("API token auth is not supported by this panel (HTTP 404 on inbounds/list): " +
			"it predates 3X-UI v3, so use username/password instead")
	default:
		return fmt.Errorf("verifying API token: panel returned HTTP %d (body: %q)", statusCode, preview(data))
	}
}

// buildLoginBody encodes credentials in the form each panel generation expects.
// v3 documents a JSON body; legacy panels take form-encoded values.
func (c *APIClient) buildLoginBody(version APIVersion) (body []byte, contentType string, err error) {
	if version == APIVersionV3 {
		payload := map[string]string{
			"username": c.panel.Username,
			"password": c.panel.Password,
		}
		data, err := json.Marshal(payload)
		if err != nil {
			return nil, "", fmt.Errorf("marshaling login body: %w", err)
		}
		return data, "application/json", nil
	}

	form := url.Values{}
	form.Set("username", c.panel.Username)
	form.Set("password", c.panel.Password)
	return []byte(form.Encode()), "application/x-www-form-urlencoded", nil
}

// parseLoginResponse turns a login reply into an APIResponse, converting the
// panel's silent failure modes into messages that name the actual cause
// instead of surfacing a bare "EOF" from the JSON decoder.
func parseLoginResponse(statusCode int, data []byte) (APIResponse, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		switch statusCode {
		case http.StatusForbidden:
			return APIResponse{}, fmt.Errorf(
				"login rejected with HTTP 403 and an empty body: the panel requires a CSRF token " +
					"(3X-UI v3+); check that base_path points at the panel root so /csrf-token is reachable, " +
					"or set api_token for this panel")
		case http.StatusNotFound:
			return APIResponse{}, fmt.Errorf(
				"login endpoint returned HTTP 404: base_path is probably wrong for this panel")
		default:
			return APIResponse{}, fmt.Errorf(
				"login returned HTTP %d with an empty body", statusCode)
		}
	}

	var apiResp APIResponse
	if err := json.Unmarshal(data, &apiResp); err != nil {
		return APIResponse{}, fmt.Errorf("decoding login response (status %d, body: %q): %w",
			statusCode, preview(data), err)
	}
	return apiResp, nil
}

// preview truncates a response body so error messages stay readable when a
// panel answers with an HTML page.
func preview(data []byte) string {
	const max = 200
	if len(data) > max {
		return string(data[:max]) + "..."
	}
	return string(data)
}

func (c *APIClient) ListInbounds(ctx context.Context) ([]Inbound, error) {
	data, err := c.do(ctx, http.MethodGet, "panel/api/inbounds/list", nil)
	if err != nil {
		return nil, fmt.Errorf("listing inbounds: %w", err)
	}

	resp, err := parseAPIResponse(data)
	if err != nil {
		return nil, fmt.Errorf("listing inbounds: %w", err)
	}

	var inbounds []Inbound
	if err := json.Unmarshal(resp.Obj, &inbounds); err != nil {
		return nil, fmt.Errorf("unmarshaling inbounds: %w", err)
	}

	return inbounds, nil
}

// AddClient creates a client on the given inbound. On v3 panels the client is
// a standalone row attached to the inbound; on legacy panels it is appended to
// the inbound's settings blob.
func (c *APIClient) AddClient(ctx context.Context, inboundID int, client Client) error {
	if err := c.ensureLoggedIn(ctx); err != nil {
		return fmt.Errorf("adding client: %w", err)
	}

	var (
		path string
		body any
	)
	if c.APIVersion() == APIVersionV3 {
		path = "panel/api/clients/add"
		body = AddClientRequestV3{Client: client, InboundIDs: []int{inboundID}}
	} else {
		settings := InboundSettings{Clients: []Client{client}}
		settingsJSON, err := json.Marshal(settings)
		if err != nil {
			return fmt.Errorf("marshaling client settings: %w", err)
		}
		path = "panel/api/inbounds/addClient"
		body = AddClientRequest{ID: inboundID, Settings: string(settingsJSON)}
	}

	data, err := c.do(ctx, http.MethodPost, path, body)
	if err != nil {
		return fmt.Errorf("adding client: %w", err)
	}
	if _, err := parseAPIResponse(data); err != nil {
		return fmt.Errorf("adding client: %w", err)
	}
	return nil
}

// GetClient fetches a v3 client row by email as raw JSON, preserving fields
// this project does not model.
func (c *APIClient) GetClient(ctx context.Context, email string) (map[string]any, error) {
	data, err := c.do(ctx, http.MethodGet, "panel/api/clients/get/"+url.PathEscape(email), nil)
	if err != nil {
		return nil, fmt.Errorf("getting client: %w", err)
	}

	resp, err := parseAPIResponse(data)
	if err != nil {
		return nil, fmt.Errorf("getting client: %w", err)
	}

	var out map[string]any
	if err := json.Unmarshal(resp.Obj, &out); err != nil {
		return nil, fmt.Errorf("unmarshaling client: %w", err)
	}
	return out, nil
}

// UpdateClient updates a client. The v3 endpoint keys off the email and
// replaces the whole row, so the panel's current record is fetched first and
// only the managed fields are overlaid — anything this project does not model
// (comments, groups, telegram ids) survives the round trip. Legacy panels key
// off the client's UUID and take a settings blob.
func (c *APIClient) UpdateClient(ctx context.Context, inboundID int, clientUUID string, client Client) error {
	if err := c.ensureLoggedIn(ctx); err != nil {
		return fmt.Errorf("updating client: %w", err)
	}

	var (
		path string
		body any
	)
	if c.APIVersion() == APIVersionV3 {
		payload, err := c.mergeClientPayload(ctx, client)
		if err != nil {
			return fmt.Errorf("updating client: %w", err)
		}
		path = "panel/api/clients/update/" + url.PathEscape(client.Email)
		body = payload
	} else {
		settings := InboundSettings{Clients: []Client{client}}
		settingsJSON, err := json.Marshal(settings)
		if err != nil {
			return fmt.Errorf("marshaling client settings: %w", err)
		}
		path = "panel/api/inbounds/updateClient/" + clientUUID
		body = AddClientRequest{ID: inboundID, Settings: string(settingsJSON)}
	}

	data, err := c.do(ctx, http.MethodPost, path, body)
	if err != nil {
		return fmt.Errorf("updating client: %w", err)
	}
	if _, err := parseAPIResponse(data); err != nil {
		return fmt.Errorf("updating client: %w", err)
	}
	return nil
}

// mergeClientPayload overlays the fields this project manages onto the panel's
// stored record, so a v3 replace-semantics update does not drop unknown fields.
func (c *APIClient) mergeClientPayload(ctx context.Context, client Client) (map[string]any, error) {
	managed, err := json.Marshal(client)
	if err != nil {
		return nil, fmt.Errorf("marshaling client: %w", err)
	}
	var overlay map[string]any
	if err := json.Unmarshal(managed, &overlay); err != nil {
		return nil, fmt.Errorf("unmarshaling client overlay: %w", err)
	}

	existing, err := c.GetClient(ctx, client.Email)
	if err != nil {
		// Fall back to sending just the managed fields; the update still
		// applies, it simply cannot preserve what we could not read.
		c.logger.Debug("could not read existing client, sending managed fields only",
			"panel", c.panel.Name, "email", client.Email, "error", err)
		return overlay, nil
	}

	payload := writablePayload(existing)
	maps.Copy(payload, overlay)
	return payload, nil
}

// writablePayload converts a client record as returned by a read into a body
// the update endpoint accepts. A read returns "id" as the row's numeric
// primary key with the secret in "uuid", whereas a write expects the secret in
// "id" — echoing the record back untouched would clobber the credential.
func writablePayload(record map[string]any) map[string]any {
	payload := maps.Clone(record)
	if payload == nil {
		return map[string]any{}
	}

	secret, _ := payload["uuid"].(string)
	for _, k := range []string{"id", "uuid", "inboundIds", "traffic", "createdAt", "updatedAt", "created_at", "updated_at"} {
		delete(payload, k)
	}
	if secret != "" {
		payload["id"] = secret
	}
	return payload
}

// PatchClient applies a partial update to a client, changing only the fields
// the patch sets. On v3 panels the patch is applied to the panel's own record
// because the update endpoint replaces the row wholesale — patching the
// partial client embedded in an inbound's settings would zero out traffic
// caps, IP limits and subscription IDs that live outside that blob.
func (c *APIClient) PatchClient(ctx context.Context, inbound Inbound, client Client, patch ClientPatch) error {
	if err := c.ensureLoggedIn(ctx); err != nil {
		return fmt.Errorf("patching client: %w", err)
	}

	if c.APIVersion() != APIVersionV3 {
		patched := client
		patch.ApplyTo(&patched)
		return c.UpdateClient(ctx, inbound.ID, ClientSecret(inbound.Protocol, patched), patched)
	}

	record, err := c.GetClient(ctx, client.Email)
	if err != nil {
		return fmt.Errorf("patching client: %w", err)
	}
	payload := writablePayload(record)
	patch.ApplyToMap(payload)

	data, err := c.do(ctx, http.MethodPost, "panel/api/clients/update/"+url.PathEscape(client.Email), payload)
	if err != nil {
		return fmt.Errorf("patching client: %w", err)
	}
	if _, err := parseAPIResponse(data); err != nil {
		return fmt.Errorf("patching client: %w", err)
	}
	return nil
}

func (c *APIClient) FetchSubscription(ctx context.Context, subId string) ([]byte, error) {
	reqURL := c.subBaseURL() + c.panel.SubPath + subId
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating subscription request: %w", err)
	}
	// v3 panels render a styled HTML page when the client asks for HTML;
	// be explicit so we always get the base64 payload.
	req.Header.Set("Accept", "text/plain")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching subscription: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading subscription body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("subscription endpoint returned status %d (body: %q)",
			resp.StatusCode, preview(body))
	}

	return body, nil
}

func (c *APIClient) GetClientTraffic(ctx context.Context, email string) (*ClientTraffic, error) {
	if err := c.ensureLoggedIn(ctx); err != nil {
		return nil, fmt.Errorf("getting client traffic: %w", err)
	}

	path := "panel/api/inbounds/getClientTraffics/" + url.PathEscape(email)
	if c.APIVersion() == APIVersionV3 {
		path = "panel/api/clients/traffic/" + url.PathEscape(email)
	}

	data, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("getting client traffic: %w", err)
	}

	resp, err := parseAPIResponse(data)
	if err != nil {
		return nil, fmt.Errorf("getting client traffic: %w", err)
	}

	var traffic ClientTraffic
	if err := json.Unmarshal(resp.Obj, &traffic); err != nil {
		return nil, fmt.Errorf("unmarshaling client traffic: %w", err)
	}

	return &traffic, nil
}

// do executes an authenticated API request with automatic re-login on session expiry.
func (c *APIClient) do(ctx context.Context, method, path string, body any) ([]byte, error) {
	if err := c.ensureLoggedIn(ctx); err != nil {
		return nil, err
	}

	var jsonBody []byte
	if body != nil {
		var err error
		jsonBody, err = json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshaling request body: %w", err)
		}
	}

	data, statusCode, err := c.executeRequest(ctx, method, path, jsonBody)
	if err != nil {
		return nil, err
	}

	// A 403 on an unsafe request is the panel's CSRF rejection, not an expired
	// session: mint a fresh token and replay once before considering re-login.
	if statusCode == http.StatusForbidden && !safeMethods[method] && c.APIVersion() == APIVersionV3 && !c.usesBearerAuth() {
		c.logger.Debug("request rejected by CSRF check, retrying with a fresh token",
			"panel", c.panel.Name, "method", method, "path", path)
		c.refreshCSRFToken(ctx)
		data, statusCode, err = c.executeRequest(ctx, method, path, jsonBody)
		if err != nil {
			return nil, err
		}
	}

	// Detect expired session and re-login once.
	// 3X-UI returns HTTP 200 with {"success":false} for expired sessions
	// (the panel almost never uses 401/3xx), so we must also check the
	// API-level success flag.
	if c.isSessionExpired(statusCode, data) {
		c.logger.Debug("session expired, re-logging in",
			"panel", c.panel.Name,
			"method", method,
			"path", path,
			"status", statusCode,
		)
		c.loggedIn.Store(false)
		// The stale session's CSRF token dies with it.
		c.clearCSRFToken()
		if err := c.ensureLoggedIn(ctx); err != nil {
			return nil, err
		}
		data, statusCode, err = c.executeRequest(ctx, method, path, jsonBody)
		if err != nil {
			return nil, err
		}
	}

	if statusCode != http.StatusOK {
		preview := string(data)
		if len(preview) > 200 {
			preview = preview[:200] + "..."
		}
		return nil, fmt.Errorf("unexpected status %d from panel (body: %q)", statusCode, preview)
	}

	return data, nil
}

// isSessionExpired checks whether the response indicates the session cookie
// has become stale and a fresh login is needed. Bearer-token panels hold no
// session, so a failure there is never fixed by re-authenticating.
// 3X-UI panels behave differently depending on version and reverse-proxy
// setup: expired sessions may produce 401, 3xx redirects, 404 (secret base
// path hiding), 200 + {"success":false}, or 200 + HTML login page.
// Instead of enumerating failures, we treat any response that is NOT a clear
// 200 + {"success":true} as a potential session issue and retry once.
func (c *APIClient) isSessionExpired(statusCode int, data []byte) bool {
	if c.usesBearerAuth() {
		return false
	}
	if statusCode == http.StatusOK && len(data) > 0 {
		var resp APIResponse
		if json.Unmarshal(data, &resp) == nil && resp.Success {
			return false // definitive success — session is alive
		}
	}
	return true
}

func (c *APIClient) ensureLoggedIn(ctx context.Context) error {
	if c.loggedIn.Load() {
		return nil
	}
	return c.Login(ctx)
}

func (c *APIClient) executeRequest(ctx context.Context, method, path string, jsonBody []byte) ([]byte, int, error) {
	reqURL := c.apiBaseURL() + c.panel.BasePath + path

	var bodyReader io.Reader
	if jsonBody != nil {
		bodyReader = bytes.NewReader(jsonBody)
	}

	req, err := http.NewRequestWithContext(ctx, method, reqURL, bodyReader)
	if err != nil {
		return nil, 0, fmt.Errorf("creating %s request: %w", method, err)
	}
	if jsonBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")

	switch {
	case c.usesBearerAuth():
		// Bearer tokens authenticate per request and skip CSRF entirely.
		req.Header.Set("Authorization", "Bearer "+c.panel.APIToken)
	case c.APIVersion() == APIVersionV3 && !safeMethods[method]:
		if token := c.csrfHeader(ctx); token != "" {
			req.Header.Set("X-CSRF-Token", token)
		}
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("executing %s request: %w", method, err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("reading response body: %w", err)
	}

	c.logger.Debug("API response",
		"panel", c.panel.Name,
		"method", method,
		"path", path,
		"status", resp.StatusCode,
		"bodyLen", len(data),
	)

	return data, resp.StatusCode, nil
}

func parseAPIResponse(data []byte) (APIResponse, error) {
	if len(data) == 0 {
		return APIResponse{}, fmt.Errorf("empty response body from panel")
	}

	var resp APIResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		// Truncate body for logging to avoid dumping huge HTML pages.
		preview := string(data)
		if len(preview) > 200 {
			preview = preview[:200] + "..."
		}
		return resp, fmt.Errorf("decoding API response (body: %q): %w", preview, err)
	}
	if !resp.Success {
		return resp, fmt.Errorf("API error: %s", resp.Msg)
	}
	return resp, nil
}
