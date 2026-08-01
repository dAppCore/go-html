package php

import (
	"context"
	"io"
	"net/http"
	"time"

	core "dappco.re/go"
	"dappco.re/go/cli/pkg/cli"
)

// bytesReaderForHTTP wraps a []byte as an io.Reader for HTTP request
// bodies. Equivalent to bytes.NewReader without importing bytes;
// core.NewBufferReader is not yet in this repo's pinned dappco.re/go
// release.
type bytesReaderForHTTP struct {
	data []byte
	pos  int
}

func (b *bytesReaderForHTTP) Read(p []byte) (int, error) {
	if b.pos >= len(b.data) {
		return 0, io.EOF
	}
	n := copy(p, b.data[b.pos:])
	b.pos += n
	return n, nil
}

func newBytesReader(data []byte) *bytesReaderForHTTP {
	return &bytesReaderForHTTP{data: data}
}

// CoolifyClient is an HTTP client for the Coolify API.
type CoolifyClient struct {
	BaseURL    string
	Token      string
	HTTPClient *http.Client
}

// CoolifyConfig holds configuration loaded from environment.
type CoolifyConfig struct {
	URL          string
	Token        string
	AppID        string
	StagingAppID string
}

// CoolifyDeployment represents a deployment from the Coolify API.
type CoolifyDeployment struct {
	ID          string    `json:"id"`
	Status      string    `json:"status"`
	CommitSHA   string    `json:"commit_sha,omitempty"`
	CommitMsg   string    `json:"commit_message,omitempty"`
	Branch      string    `json:"branch,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	FinishedAt  time.Time `json:"finished_at"`
	Log         string    `json:"log,omitempty"`
	DeployedURL string    `json:"deployed_url,omitempty"`
}

// CoolifyApp represents an application from the Coolify API.
type CoolifyApp struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	FQDN        string `json:"fqdn,omitempty"`
	Status      string `json:"status,omitempty"`
	Repository  string `json:"repository,omitempty"`
	Branch      string `json:"branch,omitempty"`
	Environment string `json:"environment,omitempty"`
}

// NewCoolifyClient creates a new Coolify API client.
func NewCoolifyClient(baseURL, token string) *CoolifyClient {
	// Ensure baseURL doesn't have trailing slash
	baseURL = trimTrailingSlash(baseURL)

	return &CoolifyClient{
		BaseURL: baseURL,
		Token:   token,
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// LoadCoolifyConfig loads Coolify configuration from .env file in the given directory.
func LoadCoolifyConfig(dir string) (*CoolifyConfig, error) { // Result boundary
	envPath := core.PathJoin(dir, ".env")
	return LoadCoolifyConfigFromFile(envPath)
}

// LoadCoolifyConfigFromFile loads Coolify configuration from a specific .env file.
func LoadCoolifyConfigFromFile(path string) (*CoolifyConfig, error) { // Result boundary
	m := getMedium()
	config := coolifyConfigFromEnv()

	// Then try .env file
	if !m.Exists(path) {
		// No .env file, just use env vars
		return validateCoolifyConfig(config)
	}

	content, err := m.Read(path)
	if err != nil {
		return nil, phpWrapAction(err, "read", ".env file")
	}

	applyCoolifyEnvFile(config, content)
	return validateCoolifyConfig(config)
}

func coolifyConfigFromEnv() *CoolifyConfig {
	return &CoolifyConfig{
		URL:          core.Getenv("COOLIFY_URL"),
		Token:        core.Getenv("COOLIFY_TOKEN"),
		AppID:        core.Getenv("COOLIFY_APP_ID"),
		StagingAppID: core.Getenv("COOLIFY_STAGING_APP_ID"),
	}
}

func applyCoolifyEnvFile(config *CoolifyConfig, content string) {
	for _, line := range core.Split(content, "\n") {
		key, value, ok := parseCoolifyEnvLine(line)
		if !ok {
			continue
		}
		setCoolifyConfigValue(config, key, value)
	}
}

func parseCoolifyEnvLine(line string) (string, string, bool) {
	line = core.Trim(line)
	if line == "" || core.HasPrefix(line, "#") {
		return "", "", false
	}

	parts := core.SplitN(line, "=", 2)
	if len(parts) != 2 {
		return "", "", false
	}

	key := core.Trim(parts[0])
	value := trimQuotes(core.Trim(parts[1]))
	return key, value, true
}

func setCoolifyConfigValue(config *CoolifyConfig, key, value string) {
	switch key {
	case "COOLIFY_URL":
		if config.URL == "" {
			config.URL = value
		}
	case "COOLIFY_TOKEN":
		if config.Token == "" {
			config.Token = value
		}
	case "COOLIFY_APP_ID":
		if config.AppID == "" {
			config.AppID = value
		}
	case "COOLIFY_STAGING_APP_ID":
		if config.StagingAppID == "" {
			config.StagingAppID = value
		}
	}
}

// validateCoolifyConfig checks that required fields are set.
func validateCoolifyConfig(config *CoolifyConfig) (*CoolifyConfig, error) { // Result boundary
	if config.URL == "" {
		return nil, phpFailure("COOLIFY_URL is not set")
	}
	if config.Token == "" {
		return nil, phpFailure("COOLIFY_TOKEN is not set")
	}
	return config, nil
}

// TriggerDeploy triggers a deployment for the specified application.
func (c *CoolifyClient) TriggerDeploy(ctx context.Context, appID string, force bool) (*CoolifyDeployment, error) { // Result boundary
	endpoint := cli.Sprintf("%s/api/v1/applications/%s/deploy", c.BaseURL, appID)

	payload := map[string]any{}
	if force {
		payload["force"] = true
	}

	bodyR := core.JSONMarshal(payload)
	if !bodyR.OK {
		return nil, phpWrapAction(bodyR.Value.(error), "marshal", "request")
	}
	body := bodyR.Value.([]byte)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, newBytesReader(body))
	if err != nil {
		return nil, phpWrapAction(err, "create", "request")
	}

	c.setHeaders(req)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, phpWrapMessage(err, requestFailedMessage)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusAccepted {
		return nil, c.parseError(resp)
	}

	respBody, _ := io.ReadAll(resp.Body)
	var deployment CoolifyDeployment
	if r := core.JSONUnmarshal(respBody, &deployment); !r.OK {
		// Some Coolify versions return minimal response
		return &CoolifyDeployment{
			Status:    "queued",
			CreatedAt: time.Now(),
		}, nil
	}

	return &deployment, nil
}

// GetDeployment retrieves a specific deployment by ID.
func (c *CoolifyClient) GetDeployment(ctx context.Context, appID, deploymentID string) (*CoolifyDeployment, error) { // Result boundary
	endpoint := cli.Sprintf("%s/api/v1/applications/%s/deployments/%s", c.BaseURL, appID, deploymentID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, phpWrapAction(err, "create", "request")
	}

	c.setHeaders(req)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, phpWrapMessage(err, requestFailedMessage)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, c.parseError(resp)
	}

	respBody, _ := io.ReadAll(resp.Body)
	var deployment CoolifyDeployment
	if r := core.JSONUnmarshal(respBody, &deployment); !r.OK {
		return nil, phpWrapAction(r.Value.(error), "decode", "response")
	}

	return &deployment, nil
}

// ListDeployments retrieves deployments for an application.
func (c *CoolifyClient) ListDeployments(ctx context.Context, appID string, limit int) ([]CoolifyDeployment, error) { // Result boundary
	endpoint := cli.Sprintf("%s/api/v1/applications/%s/deployments", c.BaseURL, appID)
	if limit > 0 {
		endpoint = cli.Sprintf("%s?limit=%d", endpoint, limit)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, phpWrapAction(err, "create", "request")
	}

	c.setHeaders(req)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, phpWrapMessage(err, requestFailedMessage)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, c.parseError(resp)
	}

	respBody, _ := io.ReadAll(resp.Body)
	var deployments []CoolifyDeployment
	if r := core.JSONUnmarshal(respBody, &deployments); !r.OK {
		return nil, phpWrapAction(r.Value.(error), "decode", "response")
	}

	return deployments, nil
}

// Rollback triggers a rollback to a previous deployment.
func (c *CoolifyClient) Rollback(ctx context.Context, appID, deploymentID string) (*CoolifyDeployment, error) { // Result boundary
	endpoint := cli.Sprintf("%s/api/v1/applications/%s/rollback", c.BaseURL, appID)

	payload := map[string]any{
		"deployment_id": deploymentID,
	}

	bodyR := core.JSONMarshal(payload)
	if !bodyR.OK {
		return nil, phpWrapAction(bodyR.Value.(error), "marshal", "request")
	}
	body := bodyR.Value.([]byte)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, newBytesReader(body))
	if err != nil {
		return nil, phpWrapAction(err, "create", "request")
	}

	c.setHeaders(req)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, phpWrapMessage(err, requestFailedMessage)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusAccepted {
		return nil, c.parseError(resp)
	}

	respBody, _ := io.ReadAll(resp.Body)
	var deployment CoolifyDeployment
	if r := core.JSONUnmarshal(respBody, &deployment); !r.OK {
		return &CoolifyDeployment{
			Status:    "rolling_back",
			CreatedAt: time.Now(),
		}, nil
	}

	return &deployment, nil
}

// GetApp retrieves application details.
func (c *CoolifyClient) GetApp(ctx context.Context, appID string) (*CoolifyApp, error) { // Result boundary
	endpoint := cli.Sprintf("%s/api/v1/applications/%s", c.BaseURL, appID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, phpWrapAction(err, "create", "request")
	}

	c.setHeaders(req)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, phpWrapMessage(err, requestFailedMessage)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, c.parseError(resp)
	}

	respBody, _ := io.ReadAll(resp.Body)
	var app CoolifyApp
	if r := core.JSONUnmarshal(respBody, &app); !r.OK {
		return nil, phpWrapAction(r.Value.(error), "decode", "response")
	}

	return &app, nil
}

// setHeaders sets common headers for API requests.
func (c *CoolifyClient) setHeaders(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
}

// parseError extracts error information from an API response.
func (c *CoolifyClient) parseError(resp *http.Response) error { // Result boundary
	body, _ := io.ReadAll(resp.Body)

	var errResp struct {
		Message string `json:"message"`
		Error   string `json:"error"`
	}

	if r := core.JSONUnmarshal(body, &errResp); r.OK {
		if errResp.Message != "" {
			return phpFailure(apiErrorFormat, resp.StatusCode, errResp.Message)
		}
		if errResp.Error != "" {
			return phpFailure(apiErrorFormat, resp.StatusCode, errResp.Error)
		}
	}

	return phpFailure(apiErrorFormat, resp.StatusCode, string(body))
}
