package xui

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"subs-aggregator/internal/config"
)

type APIClient struct {
	panel      config.PanelConfig
	httpClient *http.Client
	logger     *slog.Logger
	mu         sync.Mutex
	loggedIn   atomic.Bool
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
		},
		logger: logger,
	}
}

func (c *APIClient) PanelName() string {
	return c.panel.Name
}

func (c *APIClient) Login(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.loggedIn.Load() {
		return nil
	}

	form := url.Values{}
	form.Set("username", c.panel.Username)
	form.Set("password", c.panel.Password)

	reqURL := c.panel.Address + "/login"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("creating login request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("executing login request: %w", err)
	}
	defer resp.Body.Close()

	var apiResp APIResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return fmt.Errorf("decoding login response: %w", err)
	}

	if !apiResp.Success {
		return fmt.Errorf("login failed: %s", apiResp.Msg)
	}

	c.loggedIn.Store(true)
	c.logger.Info("logged in to panel", "panel", c.panel.Name)
	return nil
}

func (c *APIClient) ListInbounds(ctx context.Context) ([]Inbound, error) {
	data, err := c.do(ctx, http.MethodGet, "/panel/api/inbounds/list", nil)
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

func (c *APIClient) AddClient(ctx context.Context, inboundID int, client Client) error {
	settings := InboundSettings{Clients: []Client{client}}
	settingsJSON, err := json.Marshal(settings)
	if err != nil {
		return fmt.Errorf("marshaling client settings: %w", err)
	}

	addReq := AddClientRequest{
		ID:       inboundID,
		Settings: string(settingsJSON),
	}

	data, err := c.do(ctx, http.MethodPost, "/panel/api/inbounds/addClient", addReq)
	if err != nil {
		return fmt.Errorf("adding client: %w", err)
	}

	if _, err := parseAPIResponse(data); err != nil {
		return fmt.Errorf("adding client: %w", err)
	}

	return nil
}

func (c *APIClient) FetchSubscription(ctx context.Context, subId string) (string, error) {
	reqURL := c.panel.Address + c.panel.SubPath + "/" + subId
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return "", fmt.Errorf("creating subscription request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetching subscription: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading subscription body: %w", err)
	}

	return string(body), nil
}

func (c *APIClient) GetClientTraffic(ctx context.Context, email string) (*ClientTraffic, error) {
	data, err := c.do(ctx, http.MethodGet, "/panel/api/inbounds/getClientTraffics/"+email, nil)
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

// do executes an authenticated API request with automatic re-login on 401.
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

	if statusCode == http.StatusUnauthorized {
		c.loggedIn.Store(false)
		if err := c.ensureLoggedIn(ctx); err != nil {
			return nil, err
		}
		data, _, err = c.executeRequest(ctx, method, path, jsonBody)
		if err != nil {
			return nil, err
		}
	}

	return data, nil
}

func (c *APIClient) ensureLoggedIn(ctx context.Context) error {
	if c.loggedIn.Load() {
		return nil
	}
	return c.Login(ctx)
}

func (c *APIClient) executeRequest(ctx context.Context, method, path string, jsonBody []byte) ([]byte, int, error) {
	reqURL := c.panel.Address + path

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

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("executing %s request: %w", method, err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("reading response body: %w", err)
	}

	return data, resp.StatusCode, nil
}

func parseAPIResponse(data []byte) (APIResponse, error) {
	var resp APIResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return resp, fmt.Errorf("decoding API response: %w", err)
	}
	if !resp.Success {
		return resp, fmt.Errorf("API error: %s", resp.Msg)
	}
	return resp, nil
}
