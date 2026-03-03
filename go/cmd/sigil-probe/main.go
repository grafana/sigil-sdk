package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/sigil/sdks/go/sigil"
)

const (
	defaultEndpoint = "sigil-dev-001.grafana-dev.net:443"
	defaultTenantID = "4130"
	defaultUserID   = "4130"
	defaultTokenEnv = "GRAFANA_ASSISTANT_ACCESS_TOKEN"
	defaultDotEnv   = ".env"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("sigil-probe", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var endpoint string
	var tenantID string
	var userID string
	var token string
	var tokenEnv string
	var dotEnvPath string
	var verifyRead bool
	var readBaseURL string
	var readPollInterval time.Duration
	var timeout time.Duration
	var insecure bool
	var verbose bool

	fs.StringVar(&endpoint, "endpoint", defaultEndpoint, "gRPC endpoint host:port")
	fs.StringVar(&tenantID, "tenant", defaultTenantID, "tenant value sent as X-Scope-OrgID metadata")
	fs.StringVar(&userID, "user", defaultUserID, "basic auth username")
	fs.StringVar(&token, "token", "", "basic auth password/token value (overrides env/.env)")
	fs.StringVar(&tokenEnv, "token-env", defaultTokenEnv, "environment variable name that stores basic auth password/token")
	fs.StringVar(&dotEnvPath, "dotenv", defaultDotEnv, "path to .env file used as fallback when env var is empty")
	fs.BoolVar(&verifyRead, "verify-read", true, "verify read path by fetching /api/v1/generations/{id} for the HTTP push generation")
	fs.StringVar(&readBaseURL, "read-base-url", "", "HTTP API base URL (default derived from endpoint)")
	fs.DurationVar(&readPollInterval, "read-poll-interval", 500*time.Millisecond, "poll interval while waiting for read visibility")
	fs.DurationVar(&timeout, "timeout", 20*time.Second, "overall timeout for probe")
	fs.BoolVar(&insecure, "insecure", false, "use insecure plaintext gRPC transport (local/dev)")
	fs.BoolVar(&verbose, "verbose", false, "print SDK logs")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(endpoint) == "" {
		return errors.New("endpoint is required")
	}
	if strings.TrimSpace(tenantID) == "" {
		return errors.New("tenant is required")
	}
	if strings.TrimSpace(tokenEnv) == "" {
		return errors.New("token-env is required")
	}
	if strings.TrimSpace(userID) == "" {
		return errors.New("user is required")
	}
	if timeout <= 0 {
		return errors.New("timeout must be > 0")
	}
	if verifyRead && readPollInterval <= 0 {
		return errors.New("read-poll-interval must be > 0")
	}

	resolvedToken, tokenSource, err := resolveToken(token, tokenEnv, dotEnvPath)
	if err != nil {
		return err
	}

	apiBaseURL, err := resolveReadBaseURL(endpoint, readBaseURL, insecure)
	if err != nil {
		return err
	}
	httpExportEndpoint := apiBaseURL + "/api/v1/generations:export"

	now := time.Now().UTC()
	grpcGenerationID := fmt.Sprintf("grpc-probe-%d", now.UnixNano())
	httpGenerationID := fmt.Sprintf("http-probe-%d", now.UnixNano()+1)

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	results := make([]probeStepResult, 0, 4)

	grpcErr := pushGeneration(
		ctx,
		pushGenerationOptions{
			Protocol:     sigil.GenerationExportProtocolGRPC,
			Endpoint:     endpoint,
			Insecure:     insecure,
			UserID:       strings.TrimSpace(userID),
			Token:        resolvedToken,
			TenantID:     strings.TrimSpace(tenantID),
			GenerationID: grpcGenerationID,
			AgentName:    "sigil-probe-grpc",
			ProviderName: "grpc-connectivity",
			Verbose:      verbose,
			Stderr:       stderr,
		},
	)
	results = appendResult(results, "grpc_push", grpcGenerationID, grpcErr)

	httpErr := pushGeneration(
		ctx,
		pushGenerationOptions{
			Protocol:     sigil.GenerationExportProtocolHTTP,
			Endpoint:     httpExportEndpoint,
			Insecure:     insecure,
			UserID:       strings.TrimSpace(userID),
			Token:        resolvedToken,
			TenantID:     strings.TrimSpace(tenantID),
			GenerationID: httpGenerationID,
			AgentName:    "sigil-probe-http",
			ProviderName: "http-connectivity",
			Verbose:      verbose,
			Stderr:       stderr,
		},
	)
	results = appendResult(results, "http_push", httpGenerationID, httpErr)

	if verifyRead {
		if httpErr != nil {
			results = appendSkipResult(results, "http_get", httpGenerationID, "skipped because http_push failed")
		} else {
			readErr := verifyGenerationReadable(
				ctx,
				apiBaseURL,
				strings.TrimSpace(userID),
				resolvedToken,
				strings.TrimSpace(tenantID),
				httpGenerationID,
				readPollInterval,
			)
			results = appendResult(results, "http_get", httpGenerationID, readErr)
		}
	} else {
		results = appendSkipResult(results, "http_get", httpGenerationID, "disabled by -verify-read=false")
	}

	if _, err := fmt.Fprintf(
		stdout,
		"endpoint=%s\napi_base_url=%s\ntenant=%s\nuser=%s\nauth_mode=basic\ntoken_source=%s\n\n",
		endpoint,
		apiBaseURL,
		strings.TrimSpace(tenantID),
		strings.TrimSpace(userID),
		tokenSource,
	); err != nil {
		return fmt.Errorf("write probe header: %w", err)
	}
	if err := renderResultsTable(stdout, results); err != nil {
		return fmt.Errorf("write results table: %w", err)
	}

	failures := countFailedResults(results)
	if failures > 0 {
		return fmt.Errorf("%d probe step(s) failed", failures)
	}
	return nil
}

type probeStepStatus string

const (
	probeStatusOK   probeStepStatus = "ok"
	probeStatusFail probeStepStatus = "fail"
	probeStatusSkip probeStepStatus = "skip"
)

type probeStepResult struct {
	Step         string
	Status       probeStepStatus
	GenerationID string
	Detail       string
}

type pushGenerationOptions struct {
	Protocol     sigil.GenerationExportProtocol
	Endpoint     string
	Insecure     bool
	UserID       string
	Token        string
	TenantID     string
	GenerationID string
	AgentName    string
	ProviderName string
	Verbose      bool
	Stderr       io.Writer
}

func pushGeneration(ctx context.Context, options pushGenerationOptions) error {
	logBuffer := &bytes.Buffer{}
	loggerOut := io.Writer(logBuffer)
	if options.Verbose {
		loggerOut = io.MultiWriter(options.Stderr, logBuffer)
	}

	cfg := sigil.DefaultConfig()
	cfg.GenerationExport.Protocol = options.Protocol
	cfg.GenerationExport.Endpoint = options.Endpoint
	cfg.GenerationExport.Insecure = options.Insecure
	cfg.GenerationExport.BatchSize = 1
	cfg.GenerationExport.FlushInterval = time.Hour
	cfg.GenerationExport.Auth = sigil.AuthConfig{Mode: sigil.ExportAuthModeNone}
	cfg.GenerationExport.Headers = map[string]string{
		"X-Scope-OrgID": options.TenantID,
		"Authorization": formatBasicAuth(options.UserID, options.Token),
	}
	cfg.Logger = log.New(loggerOut, "", log.LstdFlags)

	client := sigil.NewClient(cfg)
	shutdownDone := false
	defer func() {
		if !shutdownDone {
			_ = client.Shutdown(context.Background())
		}
	}()

	conversationID := fmt.Sprintf("%s-conv", options.GenerationID)
	_, recorder := client.StartGeneration(ctx, sigil.GenerationStart{
		ID:             options.GenerationID,
		ConversationID: conversationID,
		AgentName:      options.AgentName,
		AgentVersion:   "0.1.0",
		Model: sigil.ModelRef{
			Provider: "probe",
			Name:     options.ProviderName,
		},
		Mode: sigil.GenerationModeSync,
		Tags: map[string]string{
			"probe": "sigil-probe",
		},
	})
	recorder.SetResult(sigil.Generation{
		Input:  []sigil.Message{sigil.UserTextMessage("ping")},
		Output: []sigil.Message{sigil.AssistantTextMessage("pong")},
	}, nil)
	recorder.End()
	if err := recorder.Err(); err != nil {
		return fmt.Errorf("local generation record failed: %w", err)
	}

	if err := client.Flush(ctx); err != nil {
		return fmt.Errorf("export failed: %w", err)
	}
	if err := client.Shutdown(ctx); err != nil {
		return fmt.Errorf("shutdown failed: %w", err)
	}
	shutdownDone = true

	if rejectionErr := findRejectionForGeneration(logBuffer.String(), options.GenerationID); rejectionErr != "" {
		return fmt.Errorf("server rejected generation %s: %s", options.GenerationID, rejectionErr)
	}
	return nil
}

func appendResult(results []probeStepResult, step, generationID string, err error) []probeStepResult {
	if err != nil {
		return append(results, probeStepResult{
			Step:         step,
			Status:       probeStatusFail,
			GenerationID: generationID,
			Detail:       err.Error(),
		})
	}
	return append(results, probeStepResult{
		Step:         step,
		Status:       probeStatusOK,
		GenerationID: generationID,
		Detail:       "success",
	})
}

func appendSkipResult(results []probeStepResult, step, generationID, detail string) []probeStepResult {
	return append(results, probeStepResult{
		Step:         step,
		Status:       probeStatusSkip,
		GenerationID: generationID,
		Detail:       detail,
	})
}

func renderResultsTable(out io.Writer, results []probeStepResult) error {
	if _, err := fmt.Fprintln(out, "| step | status | generation_id | detail |"); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(out, "| --- | --- | --- | --- |"); err != nil {
		return err
	}
	for _, result := range results {
		if _, err := fmt.Fprintf(
			out,
			"| %s | %s | %s | %s |\n",
			tableCell(result.Step),
			tableCell(string(result.Status)),
			tableCell(result.GenerationID),
			tableCell(result.Detail),
		); err != nil {
			return err
		}
	}
	return nil
}

func tableCell(input string) string {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return "-"
	}
	replaced := strings.ReplaceAll(trimmed, "|", "\\|")
	replaced = strings.ReplaceAll(replaced, "\n", " ")
	replaced = strings.ReplaceAll(replaced, "\r", " ")
	const maxLen = 180
	if len(replaced) > maxLen {
		return replaced[:maxLen-3] + "..."
	}
	return replaced
}

func countFailedResults(results []probeStepResult) int {
	count := 0
	for _, result := range results {
		if result.Status == probeStatusFail {
			count++
		}
	}
	return count
}

func resolveToken(explicitToken, tokenEnv, dotEnvPath string) (token string, source string, err error) {
	if trimmed := strings.TrimSpace(explicitToken); trimmed != "" {
		return trimmed, "flag:-token", nil
	}

	if trimmed := strings.TrimSpace(os.Getenv(tokenEnv)); trimmed != "" {
		return trimmed, "env:" + tokenEnv, nil
	}

	for _, path := range candidateDotEnvPaths(dotEnvPath) {
		value, readErr := readDotEnvValue(path, tokenEnv)
		if readErr != nil {
			if errors.Is(readErr, os.ErrNotExist) {
				continue
			}
			return "", "", fmt.Errorf("read %s: %w", path, readErr)
		}
		if strings.TrimSpace(value) == "" {
			continue
		}
		return strings.TrimSpace(value), "dotenv:" + path, nil
	}

	return "", "", fmt.Errorf("%s is empty; pass -token, export %s, or set it in %s", tokenEnv, tokenEnv, dotEnvPath)
}

func candidateDotEnvPaths(path string) []string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return nil
	}
	if filepath.IsAbs(trimmed) {
		return []string{trimmed}
	}
	return []string{
		trimmed,
		filepath.Join("..", trimmed),
		filepath.Join("..", "..", trimmed),
	}
}

func readDotEnvValue(path, key string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() {
		_ = file.Close()
	}()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		currentKey := strings.TrimSpace(parts[0])
		if currentKey != key {
			continue
		}
		return normalizeEnvValue(parts[1]), nil
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", nil
}

func normalizeEnvValue(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}

	if strings.HasPrefix(value, "\"") && strings.HasSuffix(value, "\"") && len(value) >= 2 {
		unquoted, err := strconv.Unquote(value)
		if err == nil {
			return strings.TrimSpace(unquoted)
		}
	}
	if strings.HasPrefix(value, "'") && strings.HasSuffix(value, "'") && len(value) >= 2 {
		return strings.TrimSpace(value[1 : len(value)-1])
	}

	if idx := strings.Index(value, " #"); idx >= 0 {
		value = strings.TrimSpace(value[:idx])
	}
	return value
}

func formatBasicAuth(user, token string) string {
	credentials := user + ":" + strings.TrimSpace(token)
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(credentials))
}

func resolveReadBaseURL(grpcEndpoint, readBaseURL string, insecure bool) (string, error) {
	trimmed := strings.TrimSpace(readBaseURL)
	if trimmed != "" {
		if !strings.HasPrefix(trimmed, "http://") && !strings.HasPrefix(trimmed, "https://") {
			return "", fmt.Errorf("read-base-url %q must include http:// or https://", readBaseURL)
		}
		return strings.TrimRight(trimmed, "/"), nil
	}

	host := strings.TrimSpace(grpcEndpoint)
	if host == "" {
		return "", errors.New("endpoint is required to derive read base url")
	}
	endpointScheme := ""
	if strings.Contains(host, "://") {
		parsed, err := url.Parse(host)
		if err != nil {
			return "", fmt.Errorf("parse endpoint %q: %w", grpcEndpoint, err)
		}
		endpointScheme = parsed.Scheme
		host = parsed.Host
	}

	host = strings.TrimSuffix(host, ":443")
	host = strings.TrimSuffix(host, ":80")

	scheme := "https"
	if insecure || endpointScheme == "http" {
		scheme = "http"
	}
	return scheme + "://" + host, nil
}

func verifyGenerationReadable(
	ctx context.Context,
	baseURL string,
	user string,
	token string,
	tenantID string,
	generationID string,
	pollInterval time.Duration,
) error {
	trimmedBaseURL := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if trimmedBaseURL == "" {
		return errors.New("read base url is required")
	}
	readURL := trimmedBaseURL + "/api/v1/generations/" + url.PathEscape(strings.TrimSpace(generationID))
	httpClient := &http.Client{Timeout: 10 * time.Second}

	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, readURL, nil)
		if err != nil {
			return fmt.Errorf("build read request: %w", err)
		}
		req.SetBasicAuth(user, strings.TrimSpace(token))
		req.Header.Set("X-Scope-OrgID", strings.TrimSpace(tenantID))

		resp, err := httpClient.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return fmt.Errorf("read verification timeout: %w", ctx.Err())
			}
			return fmt.Errorf("execute read request: %w", err)
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 4096))
		_ = resp.Body.Close()
		if readErr != nil {
			return fmt.Errorf("read response body: %w", readErr)
		}

		switch resp.StatusCode {
		case http.StatusOK:
			return nil
		case http.StatusNotFound, http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
			select {
			case <-ctx.Done():
				return fmt.Errorf("read verification timeout waiting for generation %s (last status=%d body=%s)", generationID, resp.StatusCode, strings.TrimSpace(string(body)))
			case <-time.After(pollInterval):
			}
			continue
		default:
			return fmt.Errorf("read verification failed status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
		}
	}
}

func findRejectionForGeneration(logOutput, generationID string) string {
	for _, line := range strings.Split(logOutput, "\n") {
		if !strings.Contains(line, "sigil generation rejected") {
			continue
		}
		if !strings.Contains(line, "id="+generationID) {
			continue
		}
		errorIndex := strings.Index(line, "error=")
		if errorIndex < 0 {
			return strings.TrimSpace(line)
		}
		return strings.TrimSpace(line[errorIndex+len("error="):])
	}
	return ""
}
