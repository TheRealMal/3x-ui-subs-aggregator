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
	"time"

	"3x-ui-sub-unifier/internal/config"
)

type APIClient struct {
	panel      config.PanelConfig
	httpClient *http.Client
	logger     *slog.Logger
	mu         sync.Mutex
	loggedIn   bool
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

func (c *APIClient) Login(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

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

	c.loggedIn = true
	c.logger.Info("logged in to panel", "panel", c.panel.Name)
	return nil
}

func (c *APIClient) ensureLoggedIn(ctx context.Context) error {
	if c.loggedIn {
		return nil
	}
	return c.Login(ctx)
}

func (c *APIClient) ListInbounds(ctx context.Context) ([]Inbound, error) {
	data, err := c.doGet(ctx, "/panel/api/inbounds/list")
	if err != nil {
		return nil, fmt.Errorf("listing inbounds: %w", err)
	}

	var apiResp APIResponse
	if err := json.Unmarshal(data, &apiResp); err != nil {
		return nil, fmt.Errorf("decoding inbounds response: %w", err)
	}

	if !apiResp.Success {
		return nil, fmt.Errorf("list inbounds failed: %s", apiResp.Msg)
	}

	var inbounds []Inbound
	if err := json.Unmarshal(apiResp.Obj, &inbounds); err != nil {
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

	data, err := c.doPost(ctx, "/panel/api/inbounds/addClient", addReq)
	if err != nil {
		return fmt.Errorf("adding client: %w", err)
	}

	var apiResp APIResponse
	if err := json.Unmarshal(data, &apiResp); err != nil {
		return fmt.Errorf("decoding add client response: %w", err)
	}

	if !apiResp.Success {
		return fmt.Errorf("add client failed: %s", apiResp.Msg)
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
	data, err := c.doGet(ctx, "/panel/api/inbounds/getClientTraffics/"+email)
	if err != nil {
		return nil, fmt.Errorf("getting client traffic: %w", err)
	}

	var apiResp APIResponse
	if err := json.Unmarshal(data, &apiResp); err != nil {
		return nil, fmt.Errorf("decoding traffic response: %w", err)
	}

	if !apiResp.Success {
		return nil, fmt.Errorf("get client traffic failed: %s", apiResp.Msg)
	}

	var traffic ClientTraffic
	if err := json.Unmarshal(apiResp.Obj, &traffic); err != nil {
		return nil, fmt.Errorf("unmarshaling client traffic: %w", err)
	}

	return &traffic, nil
}

func (c *APIClient) PanelName() string {
	return c.panel.Name
}

func (c *APIClient) doGet(ctx context.Context, path string) ([]byte, error) {
	if err := c.ensureLoggedIn(ctx); err != nil {
		return nil, err
	}

	reqURL := c.panel.Address + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating GET request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing GET request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		c.loggedIn = false
		if err := c.ensureLoggedIn(ctx); err != nil {
			return nil, err
		}

		req, err = http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
		if err != nil {
			return nil, fmt.Errorf("creating retry GET request: %w", err)
		}

		resp, err = c.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("executing retry GET request: %w", err)
		}
		defer resp.Body.Close()
	}

	return io.ReadAll(resp.Body)
}

func (c *APIClient) doPost(ctx context.Context, path string, body any) ([]byte, error) {
	if err := c.ensureLoggedIn(ctx); err != nil {
		return nil, err
	}

	reqURL := c.panel.Address + path

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshaling request body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("creating POST request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing POST request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		c.loggedIn = false
		if err := c.ensureLoggedIn(ctx); err != nil {
			return nil, err
		}

		req, err = http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(jsonBody))
		if err != nil {
			return nil, fmt.Errorf("creating retry POST request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err = c.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("executing retry POST request: %w", err)
		}
		defer resp.Body.Close()
	}

	return io.ReadAll(resp.Body)
}
