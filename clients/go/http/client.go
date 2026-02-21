// Package http provides an HTTP client for the flagz feature flag service.
package http

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	flagz "github.com/matt-riley/flagz/clients/go"
)

// Config holds configuration for the HTTP client.
type Config struct {
	// BaseURL is the base URL of the flagz server, e.g. "http://localhost:8080".
	BaseURL string
	// APIKey is the bearer token in "id.secret" format.
	APIKey string
	// HTTPClient is optional; defaults to http.DefaultClient.
	HTTPClient *http.Client
}

// Client implements flagz.FlagManager, flagz.Evaluator, and flagz.Streamer over HTTP.
type Client struct {
	cfg        Config
	httpClient *http.Client
}

// NewHTTPClient returns a new HTTP client for the flagz service.
func NewHTTPClient(cfg Config) *Client {
	hc := cfg.HTTPClient
	if hc == nil {
		hc = http.DefaultClient
	}
	return &Client{cfg: cfg, httpClient: hc}
}

// -- wire types --------------------------------------------------------------

type wireFlag struct {
	Key         string          `json:"key"`
	Description string          `json:"description"`
	Enabled     bool            `json:"enabled"`
	Variants    json.RawMessage `json:"variants"`
	Rules       json.RawMessage `json:"rules"`
	CreatedAt   string          `json:"created_at"`
	UpdatedAt   string          `json:"updated_at"`
}

type wireRule struct {
	Attribute string `json:"attribute"`
	Operator  string `json:"operator"`
	Value     any    `json:"value"`
}

type wireEvaluateReq struct {
	Key          string          `json:"key,omitempty"`
	Context      json.RawMessage `json:"context,omitempty"`
	DefaultValue bool            `json:"default_value"`
	Requests     []wireEvalReqItem `json:"requests,omitempty"`
}

type wireEvalReqItem struct {
	Key          string          `json:"key"`
	Context      json.RawMessage `json:"context,omitempty"`
	DefaultValue bool            `json:"default_value"`
}

type wireEvaluateResp struct {
	Key     string `json:"key"`
	Value   bool   `json:"value"`
	Results []struct {
		Key   string `json:"key"`
		Value bool   `json:"value"`
	} `json:"results"`
}

// -- helpers -----------------------------------------------------------------

func (c *Client) do(ctx context.Context, method, path string, body any) (*http.Response, error) {
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("flagz: marshal request: %w", err)
		}
		bodyReader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.cfg.BaseURL+path, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("flagz: create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("flagz: http: %w", err)
	}
	if resp.StatusCode >= 400 {
		defer resp.Body.Close()
		msg, _ := io.ReadAll(resp.Body)
		return nil, &APIError{StatusCode: resp.StatusCode, Message: strings.TrimSpace(string(msg))}
	}
	return resp, nil
}

// APIError is returned when the server responds with an HTTP error status.
type APIError struct {
	StatusCode int
	Message    string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("flagz: HTTP %d: %s", e.StatusCode, e.Message)
}

func decodeFlag(wf wireFlag) (flagz.Flag, error) {
	f := flagz.Flag{
		Key:         wf.Key,
		Description: wf.Description,
		Enabled:     wf.Enabled,
	}
	if wf.CreatedAt != "" {
		t, err := time.Parse(time.RFC3339, wf.CreatedAt)
		if err == nil {
			f.CreatedAt = t
		}
	}
	if wf.UpdatedAt != "" {
		t, err := time.Parse(time.RFC3339, wf.UpdatedAt)
		if err == nil {
			f.UpdatedAt = t
		}
	}
	if len(wf.Variants) > 0 && string(wf.Variants) != "null" {
		if err := json.Unmarshal(wf.Variants, &f.Variants); err != nil {
			return f, fmt.Errorf("flagz: decode variants: %w", err)
		}
	}
	if len(wf.Rules) > 0 && string(wf.Rules) != "null" {
		var wr []wireRule
		if err := json.Unmarshal(wf.Rules, &wr); err != nil {
			return f, fmt.Errorf("flagz: decode rules: %w", err)
		}
		f.Rules = make([]flagz.Rule, len(wr))
		for i, r := range wr {
			f.Rules[i] = flagz.Rule{Attribute: r.Attribute, Operator: r.Operator, Value: r.Value}
		}
	}
	return f, nil
}

func encodeFlag(f flagz.Flag) (wireFlag, error) {
	wf := wireFlag{
		Key:         f.Key,
		Description: f.Description,
		Enabled:     f.Enabled,
	}
	if len(f.Variants) > 0 {
		b, err := json.Marshal(f.Variants)
		if err != nil {
			return wf, err
		}
		wf.Variants = b
	}
	if len(f.Rules) > 0 {
		rules := make([]wireRule, len(f.Rules))
		for i, r := range f.Rules {
			rules[i] = wireRule{Attribute: r.Attribute, Operator: r.Operator, Value: r.Value}
		}
		b, err := json.Marshal(rules)
		if err != nil {
			return wf, err
		}
		wf.Rules = b
	}
	return wf, nil
}

// -- FlagManager -------------------------------------------------------------

func (c *Client) CreateFlag(ctx context.Context, flag flagz.Flag) (flagz.Flag, error) {
	wf, err := encodeFlag(flag)
	if err != nil {
		return flagz.Flag{}, err
	}
	resp, err := c.do(ctx, http.MethodPost, "/v1/flags", map[string]any{"flag": wf})
	if err != nil {
		return flagz.Flag{}, err
	}
	defer resp.Body.Close()
	var out struct {
		Flag wireFlag `json:"flag"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return flagz.Flag{}, fmt.Errorf("flagz: decode response: %w", err)
	}
	return decodeFlag(out.Flag)
}

func (c *Client) GetFlag(ctx context.Context, key string) (flagz.Flag, error) {
	resp, err := c.do(ctx, http.MethodGet, "/v1/flags/"+key, nil)
	if err != nil {
		return flagz.Flag{}, err
	}
	defer resp.Body.Close()
	var out struct {
		Flag wireFlag `json:"flag"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return flagz.Flag{}, fmt.Errorf("flagz: decode response: %w", err)
	}
	return decodeFlag(out.Flag)
}

func (c *Client) ListFlags(ctx context.Context) ([]flagz.Flag, error) {
	resp, err := c.do(ctx, http.MethodGet, "/v1/flags", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out struct {
		Flags []wireFlag `json:"flags"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("flagz: decode response: %w", err)
	}
	flags := make([]flagz.Flag, 0, len(out.Flags))
	for _, wf := range out.Flags {
		f, err := decodeFlag(wf)
		if err != nil {
			return nil, err
		}
		flags = append(flags, f)
	}
	return flags, nil
}

func (c *Client) UpdateFlag(ctx context.Context, flag flagz.Flag) (flagz.Flag, error) {
	wf, err := encodeFlag(flag)
	if err != nil {
		return flagz.Flag{}, err
	}
	resp, err := c.do(ctx, http.MethodPut, "/v1/flags/"+flag.Key, map[string]any{"flag": wf})
	if err != nil {
		return flagz.Flag{}, err
	}
	defer resp.Body.Close()
	var out struct {
		Flag wireFlag `json:"flag"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return flagz.Flag{}, fmt.Errorf("flagz: decode response: %w", err)
	}
	return decodeFlag(out.Flag)
}

func (c *Client) DeleteFlag(ctx context.Context, key string) error {
	resp, err := c.do(ctx, http.MethodDelete, "/v1/flags/"+key, nil)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// -- Evaluator ---------------------------------------------------------------

func (c *Client) Evaluate(ctx context.Context, key string, evalCtx flagz.EvaluationContext, defaultValue bool) (bool, error) {
	ctxJSON, err := json.Marshal(evalCtx)
	if err != nil {
		return defaultValue, fmt.Errorf("flagz: marshal context: %w", err)
	}
	body := wireEvaluateReq{
		Key:          key,
		Context:      ctxJSON,
		DefaultValue: defaultValue,
	}
	resp, err := c.do(ctx, http.MethodPost, "/v1/evaluate", body)
	if err != nil {
		return defaultValue, err
	}
	defer resp.Body.Close()
	var out wireEvaluateResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return defaultValue, fmt.Errorf("flagz: decode response: %w", err)
	}
	return out.Value, nil
}

func (c *Client) EvaluateBatch(ctx context.Context, reqs []flagz.EvaluateRequest) ([]flagz.EvaluateResult, error) {
	items := make([]wireEvalReqItem, len(reqs))
	for i, r := range reqs {
		ctxJSON, err := json.Marshal(r.Context)
		if err != nil {
			return nil, fmt.Errorf("flagz: marshal context: %w", err)
		}
		items[i] = wireEvalReqItem{Key: r.Key, Context: ctxJSON, DefaultValue: r.DefaultValue}
	}
	body := wireEvaluateReq{Requests: items}
	resp, err := c.do(ctx, http.MethodPost, "/v1/evaluate", body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out wireEvaluateResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("flagz: decode response: %w", err)
	}
	results := make([]flagz.EvaluateResult, len(out.Results))
	for i, r := range out.Results {
		results[i] = flagz.EvaluateResult{Key: r.Key, Value: r.Value}
	}
	return results, nil
}

// -- Streamer ----------------------------------------------------------------

// Stream connects to the SSE stream and emits FlagEvents on the returned channel.
// The channel is closed when ctx is cancelled or the connection drops.
func (c *Client) Stream(ctx context.Context, lastEventID int64) (<-chan flagz.FlagEvent, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.cfg.BaseURL+"/v1/stream", nil)
	if err != nil {
		return nil, fmt.Errorf("flagz: create stream request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	if lastEventID > 0 {
		req.Header.Set("Last-Event-ID", fmt.Sprintf("%d", lastEventID))
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("flagz: stream connect: %w", err)
	}
	if resp.StatusCode >= 400 {
		defer resp.Body.Close()
		msg, _ := io.ReadAll(resp.Body)
		return nil, &APIError{StatusCode: resp.StatusCode, Message: strings.TrimSpace(string(msg))}
	}

	ch := make(chan flagz.FlagEvent, 16)
	go func() {
		defer close(ch)
		defer resp.Body.Close()
		// Use a buffered reader with a 1 MiB buffer to handle large SSE data lines.
		br := bufio.NewReaderSize(resp.Body, 1<<20)
		parseSSE(ctx, br, ch)
	}()
	return ch, nil
}

// parseSSE reads SSE lines from r and sends parsed FlagEvents to ch.
// It implements the subset of the SSE spec used by the flagz server:
// id, event, data fields; blank-line flush; multi-line data concatenation.
func parseSSE(ctx context.Context, r *bufio.Reader, ch chan<- flagz.FlagEvent) {
	var (
		eventType string
		dataLines []string
		eventID   int64
	)

	for {
		if ctx.Err() != nil {
			return
		}
		line, err := r.ReadString('\n')
		line = strings.TrimRight(line, "\r\n")

		if line == "" {
			// Blank line: dispatch event if we have data.
			if len(dataLines) > 0 {
				data := strings.Join(dataLines, "\n")
				ev := flagz.FlagEvent{Type: eventType, EventID: eventID}
				if eventType == "update" || eventType == "delete" {
					var f flagz.Flag
					if jsonErr := json.Unmarshal([]byte(data), &f); jsonErr == nil {
						ev.Flag = &f
						ev.Key = f.Key
					}
				}
				select {
				case ch <- ev:
				case <-ctx.Done():
					return
				}
			}
			// Reset for next event.
			eventType = ""
			dataLines = nil
		} else if strings.HasPrefix(line, "id:") {
			fmt.Sscanf(strings.TrimSpace(strings.TrimPrefix(line, "id:")), "%d", &eventID)
		} else if strings.HasPrefix(line, "event:") {
			eventType = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		} else if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}

		if err != nil {
			return
		}
	}
}
