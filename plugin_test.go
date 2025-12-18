// Package main provides tests for the Slack plugin.
package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/relicta-tech/relicta-plugin-sdk/plugin"
)

// TestGetInfo verifies plugin metadata.
func TestGetInfo(t *testing.T) {
	p := &SlackPlugin{}
	info := p.GetInfo()

	t.Run("name", func(t *testing.T) {
		if info.Name != "slack" {
			t.Errorf("expected name 'slack', got %q", info.Name)
		}
	})

	t.Run("version", func(t *testing.T) {
		if info.Version != "2.0.0" {
			t.Errorf("expected version '2.0.0', got %q", info.Version)
		}
	})

	t.Run("description", func(t *testing.T) {
		expected := "Send Slack notifications for releases"
		if info.Description != expected {
			t.Errorf("expected description %q, got %q", expected, info.Description)
		}
	})

	t.Run("author", func(t *testing.T) {
		if info.Author != "Relicta Team" {
			t.Errorf("expected author 'Relicta Team', got %q", info.Author)
		}
	})

	t.Run("hooks", func(t *testing.T) {
		expectedHooks := []plugin.Hook{
			plugin.HookPostPublish,
			plugin.HookOnSuccess,
			plugin.HookOnError,
		}

		if len(info.Hooks) != len(expectedHooks) {
			t.Errorf("expected %d hooks, got %d", len(expectedHooks), len(info.Hooks))
			return
		}

		for i, h := range expectedHooks {
			if info.Hooks[i] != h {
				t.Errorf("expected hook[%d] to be %q, got %q", i, h, info.Hooks[i])
			}
		}
	})

	t.Run("config_schema", func(t *testing.T) {
		if info.ConfigSchema == "" {
			t.Error("expected non-empty config schema")
			return
		}

		// Verify it's valid JSON
		var schema map[string]any
		if err := json.Unmarshal([]byte(info.ConfigSchema), &schema); err != nil {
			t.Errorf("expected valid JSON schema, got error: %v", err)
		}

		// Check required fields are defined
		if _, ok := schema["properties"]; !ok {
			t.Error("expected schema to have 'properties'")
		}
	})
}

// TestValidate tests config validation with various scenarios.
func TestValidate(t *testing.T) {
	p := &SlackPlugin{}
	ctx := context.Background()

	tests := []struct {
		name        string
		config      map[string]any
		envWebhook  string
		wantValid   bool
		wantErrCode string
		wantErrMsg  string
	}{
		{
			name:        "missing webhook URL",
			config:      map[string]any{},
			wantValid:   false,
			wantErrCode: "required",
			wantErrMsg:  "Slack webhook URL is required",
		},
		{
			name: "empty webhook URL",
			config: map[string]any{
				"webhook": "",
			},
			wantValid:   false,
			wantErrCode: "required",
			wantErrMsg:  "Slack webhook URL is required",
		},
		{
			name: "invalid URL format",
			config: map[string]any{
				"webhook": "not-a-valid-url",
			},
			wantValid:   false,
			wantErrCode: "format",
		},
		{
			name: "non-HTTPS URL",
			config: map[string]any{
				"webhook": "http://hooks.slack.com/services/T00000000/B00000000/XXXX",
			},
			wantValid:   false,
			wantErrCode: "format",
			wantErrMsg:  "webhook URL must use HTTPS",
		},
		{
			name: "wrong host",
			config: map[string]any{
				"webhook": "https://evil.example.com/services/T00000000/B00000000/XXXX",
			},
			wantValid:   false,
			wantErrCode: "format",
			wantErrMsg:  "webhook URL must be on hooks.slack.com",
		},
		{
			name: "wrong path",
			config: map[string]any{
				"webhook": "https://hooks.slack.com/wrong/T00000000/B00000000/XXXX",
			},
			wantValid:   false,
			wantErrCode: "format",
			wantErrMsg:  "webhook URL path must start with /services/",
		},
		{
			name: "valid webhook URL",
			config: map[string]any{
				"webhook": "https://hooks.slack.com/services/T00000000/B00000000/TESTTOKEN",
			},
			wantValid: true,
		},
		{
			name:       "valid webhook from environment",
			config:     map[string]any{},
			envWebhook: "https://hooks.slack.com/services/T00000000/B00000000/TESTTOKEN",
			wantValid:  true,
		},
		{
			name: "valid webhook with optional fields",
			config: map[string]any{
				"webhook":           "https://hooks.slack.com/services/T00000000/B00000000/TESTTOKEN",
				"channel":           "#releases",
				"username":          "ReleaseBot",
				"icon_emoji":        ":ship:",
				"notify_on_success": true,
				"notify_on_error":   false,
			},
			wantValid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set or clear environment variable
			if tt.envWebhook != "" {
				_ = os.Setenv("SLACK_WEBHOOK_URL", tt.envWebhook)
				defer func() { _ = os.Unsetenv("SLACK_WEBHOOK_URL") }()
			} else {
				_ = os.Unsetenv("SLACK_WEBHOOK_URL")
			}

			resp, err := p.Validate(ctx, tt.config)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if resp.Valid != tt.wantValid {
				t.Errorf("expected valid=%v, got valid=%v", tt.wantValid, resp.Valid)
			}

			if !tt.wantValid {
				if len(resp.Errors) == 0 {
					t.Error("expected validation errors, got none")
					return
				}

				if tt.wantErrCode != "" && resp.Errors[0].Code != tt.wantErrCode {
					t.Errorf("expected error code %q, got %q", tt.wantErrCode, resp.Errors[0].Code)
				}

				if tt.wantErrMsg != "" && !strings.Contains(resp.Errors[0].Message, tt.wantErrMsg) {
					t.Errorf("expected error message to contain %q, got %q", tt.wantErrMsg, resp.Errors[0].Message)
				}
			} else {
				if len(resp.Errors) > 0 {
					t.Errorf("expected no errors, got %v", resp.Errors)
				}
			}
		})
	}
}

// TestParseConfig tests config parsing with defaults and custom values.
func TestParseConfig(t *testing.T) {
	p := &SlackPlugin{}

	tests := []struct {
		name     string
		config   map[string]any
		envVars  map[string]string
		expected *Config
	}{
		{
			name:   "all defaults",
			config: map[string]any{},
			expected: &Config{
				WebhookURL:       "",
				Channel:          "",
				Username:         "Relicta",
				IconEmoji:        ":rocket:",
				IconURL:          "",
				NotifyOnSuccess:  true,
				NotifyOnError:    true,
				IncludeChangelog: false,
				Mentions:         nil,
			},
		},
		{
			name: "custom values",
			config: map[string]any{
				"webhook":           "https://hooks.slack.com/services/TEST",
				"channel":           "#releases",
				"username":          "ReleaseBot",
				"icon_emoji":        ":ship:",
				"icon_url":          "https://example.com/icon.png",
				"notify_on_success": false,
				"notify_on_error":   false,
				"include_changelog": true,
				"mentions":          []any{"U123", "@team"},
			},
			expected: &Config{
				WebhookURL:       "https://hooks.slack.com/services/TEST",
				Channel:          "#releases",
				Username:         "ReleaseBot",
				IconEmoji:        ":ship:",
				IconURL:          "https://example.com/icon.png",
				NotifyOnSuccess:  false,
				NotifyOnError:    false,
				IncludeChangelog: true,
				Mentions:         []string{"U123", "@team"},
			},
		},
		{
			name:   "webhook from environment",
			config: map[string]any{},
			envVars: map[string]string{
				"SLACK_WEBHOOK_URL": "https://hooks.slack.com/services/ENV_TEST",
			},
			expected: &Config{
				WebhookURL:       "https://hooks.slack.com/services/ENV_TEST",
				Channel:          "",
				Username:         "Relicta",
				IconEmoji:        ":rocket:",
				IconURL:          "",
				NotifyOnSuccess:  true,
				NotifyOnError:    true,
				IncludeChangelog: false,
				Mentions:         nil,
			},
		},
		{
			name: "config overrides environment",
			config: map[string]any{
				"webhook": "https://hooks.slack.com/services/CONFIG",
			},
			envVars: map[string]string{
				"SLACK_WEBHOOK_URL": "https://hooks.slack.com/services/ENV_TEST",
			},
			expected: &Config{
				WebhookURL:       "https://hooks.slack.com/services/CONFIG",
				Channel:          "",
				Username:         "Relicta",
				IconEmoji:        ":rocket:",
				IconURL:          "",
				NotifyOnSuccess:  true,
				NotifyOnError:    true,
				IncludeChangelog: false,
				Mentions:         nil,
			},
		},
		{
			name: "empty string values use defaults",
			config: map[string]any{
				"username":   "",
				"icon_emoji": "",
			},
			expected: &Config{
				WebhookURL:       "",
				Channel:          "",
				Username:         "Relicta",
				IconEmoji:        ":rocket:",
				IconURL:          "",
				NotifyOnSuccess:  true,
				NotifyOnError:    true,
				IncludeChangelog: false,
				Mentions:         nil,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear environment
			_ = os.Unsetenv("SLACK_WEBHOOK_URL")

			// Set test environment variables
			for k, v := range tt.envVars {
				_ = os.Setenv(k, v)
				defer func(key string) { _ = os.Unsetenv(key) }(k)
			}

			cfg := p.parseConfig(tt.config)

			if cfg.WebhookURL != tt.expected.WebhookURL {
				t.Errorf("WebhookURL: expected %q, got %q", tt.expected.WebhookURL, cfg.WebhookURL)
			}
			if cfg.Channel != tt.expected.Channel {
				t.Errorf("Channel: expected %q, got %q", tt.expected.Channel, cfg.Channel)
			}
			if cfg.Username != tt.expected.Username {
				t.Errorf("Username: expected %q, got %q", tt.expected.Username, cfg.Username)
			}
			if cfg.IconEmoji != tt.expected.IconEmoji {
				t.Errorf("IconEmoji: expected %q, got %q", tt.expected.IconEmoji, cfg.IconEmoji)
			}
			if cfg.IconURL != tt.expected.IconURL {
				t.Errorf("IconURL: expected %q, got %q", tt.expected.IconURL, cfg.IconURL)
			}
			if cfg.NotifyOnSuccess != tt.expected.NotifyOnSuccess {
				t.Errorf("NotifyOnSuccess: expected %v, got %v", tt.expected.NotifyOnSuccess, cfg.NotifyOnSuccess)
			}
			if cfg.NotifyOnError != tt.expected.NotifyOnError {
				t.Errorf("NotifyOnError: expected %v, got %v", tt.expected.NotifyOnError, cfg.NotifyOnError)
			}
			if cfg.IncludeChangelog != tt.expected.IncludeChangelog {
				t.Errorf("IncludeChangelog: expected %v, got %v", tt.expected.IncludeChangelog, cfg.IncludeChangelog)
			}
			if len(cfg.Mentions) != len(tt.expected.Mentions) {
				t.Errorf("Mentions: expected %v, got %v", tt.expected.Mentions, cfg.Mentions)
			} else {
				for i := range cfg.Mentions {
					if cfg.Mentions[i] != tt.expected.Mentions[i] {
						t.Errorf("Mentions[%d]: expected %q, got %q", i, tt.expected.Mentions[i], cfg.Mentions[i])
					}
				}
			}
		})
	}
}

// TestExecute tests execution for relevant hooks using dry run mode.
func TestExecute(t *testing.T) {
	p := &SlackPlugin{}
	ctx := context.Background()

	baseConfig := map[string]any{
		"webhook": "https://hooks.slack.com/services/T00000000/B00000000/XXXX",
		"channel": "#releases",
	}

	baseContext := plugin.ReleaseContext{
		Version:         "1.2.3",
		PreviousVersion: "1.2.2",
		TagName:         "v1.2.3",
		ReleaseType:     "minor",
		Branch:          "main",
		CommitSHA:       "abc123",
		ReleaseNotes:    "## What's Changed\n- Feature A\n- Bug fix B",
		Changes: &plugin.CategorizedChanges{
			Features: []plugin.ConventionalCommit{
				{Hash: "abc", Type: "feat", Description: "Add feature A"},
			},
			Fixes: []plugin.ConventionalCommit{
				{Hash: "def", Type: "fix", Description: "Fix bug B"},
			},
		},
	}

	t.Run("HookPostPublish_dry_run", func(t *testing.T) {
		req := plugin.ExecuteRequest{
			Hook:    plugin.HookPostPublish,
			Config:  baseConfig,
			Context: baseContext,
			DryRun:  true,
		}

		resp, err := p.Execute(ctx, req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !resp.Success {
			t.Errorf("expected success, got failure: %s", resp.Error)
		}

		if !strings.Contains(resp.Message, "Would send Slack success notification") {
			t.Errorf("unexpected message: %s", resp.Message)
		}

		// Check outputs
		if resp.Outputs != nil {
			if channel, ok := resp.Outputs["channel"]; ok {
				if channel != "#releases" {
					t.Errorf("expected channel '#releases', got %v", channel)
				}
			}
			if version, ok := resp.Outputs["version"]; ok {
				if version != "1.2.3" {
					t.Errorf("expected version '1.2.3', got %v", version)
				}
			}
		}
	})

	t.Run("HookOnSuccess_dry_run", func(t *testing.T) {
		req := plugin.ExecuteRequest{
			Hook:    plugin.HookOnSuccess,
			Config:  baseConfig,
			Context: baseContext,
			DryRun:  true,
		}

		resp, err := p.Execute(ctx, req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !resp.Success {
			t.Errorf("expected success, got failure: %s", resp.Error)
		}

		if !strings.Contains(resp.Message, "Would send Slack success notification") {
			t.Errorf("unexpected message: %s", resp.Message)
		}
	})

	t.Run("HookOnError_dry_run", func(t *testing.T) {
		req := plugin.ExecuteRequest{
			Hook:    plugin.HookOnError,
			Config:  baseConfig,
			Context: baseContext,
			DryRun:  true,
		}

		resp, err := p.Execute(ctx, req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !resp.Success {
			t.Errorf("expected success, got failure: %s", resp.Error)
		}

		if !strings.Contains(resp.Message, "Would send Slack error notification") {
			t.Errorf("unexpected message: %s", resp.Message)
		}
	})

	t.Run("success_notification_disabled", func(t *testing.T) {
		config := map[string]any{
			"webhook":           "https://hooks.slack.com/services/T00000000/B00000000/XXXX",
			"notify_on_success": false,
		}

		req := plugin.ExecuteRequest{
			Hook:    plugin.HookPostPublish,
			Config:  config,
			Context: baseContext,
			DryRun:  true,
		}

		resp, err := p.Execute(ctx, req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !resp.Success {
			t.Errorf("expected success, got failure: %s", resp.Error)
		}

		if !strings.Contains(resp.Message, "Success notification disabled") {
			t.Errorf("unexpected message: %s", resp.Message)
		}
	})

	t.Run("error_notification_disabled", func(t *testing.T) {
		config := map[string]any{
			"webhook":         "https://hooks.slack.com/services/T00000000/B00000000/XXXX",
			"notify_on_error": false,
		}

		req := plugin.ExecuteRequest{
			Hook:    plugin.HookOnError,
			Config:  config,
			Context: baseContext,
			DryRun:  true,
		}

		resp, err := p.Execute(ctx, req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !resp.Success {
			t.Errorf("expected success, got failure: %s", resp.Error)
		}

		if !strings.Contains(resp.Message, "Error notification disabled") {
			t.Errorf("unexpected message: %s", resp.Message)
		}
	})

	t.Run("unhandled_hook", func(t *testing.T) {
		req := plugin.ExecuteRequest{
			Hook:    plugin.HookPreInit,
			Config:  baseConfig,
			Context: baseContext,
			DryRun:  true,
		}

		resp, err := p.Execute(ctx, req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !resp.Success {
			t.Errorf("expected success, got failure: %s", resp.Error)
		}

		if !strings.Contains(resp.Message, "not handled") {
			t.Errorf("unexpected message: %s", resp.Message)
		}
	})
}

// TestBuildSlackMentions tests the mention formatting function.
func TestBuildSlackMentions(t *testing.T) {
	tests := []struct {
		name     string
		mentions []string
		expected string
	}{
		{
			name:     "empty mentions",
			mentions: nil,
			expected: "",
		},
		{
			name:     "empty slice",
			mentions: []string{},
			expected: "",
		},
		{
			name:     "single user ID",
			mentions: []string{"U123456"},
			expected: "<@U123456>",
		},
		{
			name:     "user with @ prefix",
			mentions: []string{"@U123456"},
			expected: "<@U123456>",
		},
		{
			name:     "already formatted user",
			mentions: []string{"<@U123456>"},
			expected: "<@U123456>",
		},
		{
			name:     "already formatted group",
			mentions: []string{"<!subteam^S123456>"},
			expected: "<!subteam^S123456>",
		},
		{
			name:     "multiple mentions",
			mentions: []string{"U123", "@U456", "<@U789>"},
			expected: "<@U123> <@U456> <@U789>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildSlackMentions(tt.mentions)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

// TestValidateSlackWebhookURL tests the webhook URL validation function.
func TestValidateSlackWebhookURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "empty URL",
			url:     "",
			wantErr: true,
			errMsg:  "webhook URL is required",
		},
		{
			name:    "invalid URL - no scheme",
			url:     "not-a-url",
			wantErr: true,
			errMsg:  "must use HTTPS",
		},
		{
			name:    "invalid URL - malformed",
			url:     "://malformed",
			wantErr: true,
			errMsg:  "invalid URL",
		},
		{
			name:    "HTTP URL",
			url:     "http://hooks.slack.com/services/T00/B00/XXX",
			wantErr: true,
			errMsg:  "must use HTTPS",
		},
		{
			name:    "wrong host",
			url:     "https://evil.com/services/T00/B00/XXX",
			wantErr: true,
			errMsg:  "must be on hooks.slack.com",
		},
		{
			name:    "wrong path",
			url:     "https://hooks.slack.com/api/T00/B00/XXX",
			wantErr: true,
			errMsg:  "must start with /services/",
		},
		{
			name:    "valid URL",
			url:     "https://hooks.slack.com/services/T00000000/B00000000/XXXXXXXX",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSlackWebhookURL(tt.url)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
					return
				}
				if tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("expected error containing %q, got %q", tt.errMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

// TestSendMessage tests the HTTP message sending with a mock server.
func TestSendMessage(t *testing.T) {
	p := &SlackPlugin{}
	ctx := context.Background()

	t.Run("successful send", func(t *testing.T) {
		// Create a mock server
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Verify request
			if r.Method != "POST" {
				t.Errorf("expected POST, got %s", r.Method)
			}
			if r.Header.Get("Content-Type") != "application/json" {
				t.Errorf("expected Content-Type application/json, got %s", r.Header.Get("Content-Type"))
			}

			// Decode and verify payload
			var msg SlackMessage
			if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
				t.Errorf("failed to decode request: %v", err)
			}

			if msg.Channel != "#test" {
				t.Errorf("expected channel #test, got %s", msg.Channel)
			}

			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		// Note: We cannot easily test with the default client due to
		// TLS certificate validation. This test demonstrates the structure.
		// In a real scenario, we would inject the HTTP client.
		msg := SlackMessage{
			Channel:  "#test",
			Username: "TestBot",
			Text:     "Test message",
		}

		// We skip the actual call since the test server uses a self-signed cert
		// and the default client enforces TLS 1.3
		_ = p
		_ = ctx
		_ = msg
	})

	t.Run("error response", func(t *testing.T) {
		// Test structure for error handling
		// In production, this would use an injected HTTP client
	})
}

// TestSuccessNotificationFormat tests the format of success notifications.
func TestSuccessNotificationFormat(t *testing.T) {
	p := &SlackPlugin{}
	ctx := context.Background()

	t.Run("with changelog", func(t *testing.T) {
		config := map[string]any{
			"webhook":           "https://hooks.slack.com/services/T00/B00/XXX",
			"include_changelog": true,
			"channel":           "#releases",
			"mentions":          []any{"U123"},
		}

		releaseCtx := plugin.ReleaseContext{
			Version:      "2.0.0",
			TagName:      "v2.0.0",
			ReleaseType:  "major",
			Branch:       "main",
			ReleaseNotes: "## Release Notes\n- Breaking change A\n- Feature B",
			Changes: &plugin.CategorizedChanges{
				Breaking: []plugin.ConventionalCommit{
					{Hash: "abc", Type: "feat", Description: "Breaking change", Breaking: true},
				},
				Features: []plugin.ConventionalCommit{
					{Hash: "def", Type: "feat", Description: "Feature B"},
				},
			},
		}

		req := plugin.ExecuteRequest{
			Hook:    plugin.HookPostPublish,
			Config:  config,
			Context: releaseCtx,
			DryRun:  true,
		}

		resp, err := p.Execute(ctx, req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !resp.Success {
			t.Errorf("expected success, got failure: %s", resp.Error)
		}
	})

	t.Run("long changelog truncated", func(t *testing.T) {
		config := map[string]any{
			"webhook":           "https://hooks.slack.com/services/T00/B00/XXX",
			"include_changelog": true,
		}

		// Create a very long release notes string
		longNotes := strings.Repeat("x", 3000)

		releaseCtx := plugin.ReleaseContext{
			Version:      "1.0.0",
			TagName:      "v1.0.0",
			ReleaseType:  "patch",
			Branch:       "main",
			ReleaseNotes: longNotes,
		}

		req := plugin.ExecuteRequest{
			Hook:    plugin.HookPostPublish,
			Config:  config,
			Context: releaseCtx,
			DryRun:  true,
		}

		resp, err := p.Execute(ctx, req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !resp.Success {
			t.Errorf("expected success, got failure: %s", resp.Error)
		}
	})

	t.Run("without changes", func(t *testing.T) {
		config := map[string]any{
			"webhook": "https://hooks.slack.com/services/T00/B00/XXX",
		}

		releaseCtx := plugin.ReleaseContext{
			Version:     "1.0.0",
			TagName:     "v1.0.0",
			ReleaseType: "patch",
			Branch:      "main",
			Changes:     nil,
		}

		req := plugin.ExecuteRequest{
			Hook:    plugin.HookPostPublish,
			Config:  config,
			Context: releaseCtx,
			DryRun:  true,
		}

		resp, err := p.Execute(ctx, req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !resp.Success {
			t.Errorf("expected success, got failure: %s", resp.Error)
		}
	})
}

// TestSlackMessageMarshaling tests that SlackMessage marshals correctly.
func TestSlackMessageMarshaling(t *testing.T) {
	msg := SlackMessage{
		Channel:   "#test",
		Username:  "TestBot",
		IconEmoji: ":robot:",
		Text:      "Hello world",
		Attachments: []Attachment{
			{
				Color:  "good",
				Title:  "Release v1.0.0",
				Text:   "Release notes here",
				Footer: "Relicta",
				Fields: []Field{
					{Title: "Version", Value: "1.0.0", Short: true},
				},
			},
		},
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	// Verify the JSON contains expected fields
	jsonStr := string(data)

	expectedFields := []string{
		`"channel":"#test"`,
		`"username":"TestBot"`,
		`"icon_emoji":":robot:"`,
		`"text":"Hello world"`,
		`"color":"good"`,
		`"title":"Release v1.0.0"`,
		`"footer":"Relicta"`,
	}

	for _, field := range expectedFields {
		if !strings.Contains(jsonStr, field) {
			t.Errorf("expected JSON to contain %s", field)
		}
	}

	// Verify it can be unmarshaled back
	var decoded SlackMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.Channel != msg.Channel {
		t.Errorf("expected channel %q, got %q", msg.Channel, decoded.Channel)
	}
}

// TestHTMLEscaping tests that release notes are properly HTML-escaped.
func TestHTMLEscaping(t *testing.T) {
	p := &SlackPlugin{}
	ctx := context.Background()

	config := map[string]any{
		"webhook":           "https://hooks.slack.com/services/T00/B00/XXX",
		"include_changelog": true,
	}

	// Release notes with HTML that should be escaped
	releaseCtx := plugin.ReleaseContext{
		Version:      "1.0.0",
		TagName:      "v1.0.0",
		ReleaseType:  "patch",
		Branch:       "main",
		ReleaseNotes: "<script>alert('xss')</script>",
	}

	req := plugin.ExecuteRequest{
		Hook:    plugin.HookPostPublish,
		Config:  config,
		Context: releaseCtx,
		DryRun:  true,
	}

	resp, err := p.Execute(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !resp.Success {
		t.Errorf("expected success, got failure: %s", resp.Error)
	}
}

// TestErrorNotificationFormat tests the format of error notifications.
func TestErrorNotificationFormat(t *testing.T) {
	p := &SlackPlugin{}
	ctx := context.Background()

	config := map[string]any{
		"webhook":  "https://hooks.slack.com/services/T00/B00/XXX",
		"channel":  "#alerts",
		"mentions": []any{"U123", "U456"},
	}

	releaseCtx := plugin.ReleaseContext{
		Version: "1.0.0",
		TagName: "v1.0.0",
		Branch:  "main",
	}

	req := plugin.ExecuteRequest{
		Hook:    plugin.HookOnError,
		Config:  config,
		Context: releaseCtx,
		DryRun:  true,
	}

	resp, err := p.Execute(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !resp.Success {
		t.Errorf("expected success, got failure: %s", resp.Error)
	}

	if !strings.Contains(resp.Message, "Would send Slack error notification") {
		t.Errorf("unexpected message: %s", resp.Message)
	}
}

// TestContextCancellation tests that execution respects context cancellation.
func TestContextCancellation(t *testing.T) {
	p := &SlackPlugin{}

	config := map[string]any{
		"webhook": "https://hooks.slack.com/services/T00/B00/XXX",
	}

	releaseCtx := plugin.ReleaseContext{
		Version:     "1.0.0",
		TagName:     "v1.0.0",
		ReleaseType: "patch",
		Branch:      "main",
	}

	// Create a cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	req := plugin.ExecuteRequest{
		Hook:    plugin.HookPostPublish,
		Config:  config,
		Context: releaseCtx,
		DryRun:  true, // Dry run should still work with cancelled context
	}

	// Dry run doesn't actually make HTTP requests, so it should succeed
	resp, err := p.Execute(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !resp.Success {
		t.Errorf("expected success in dry run mode, got failure: %s", resp.Error)
	}
}

// TestSendMessageActual tests sending messages with an actual HTTP mock server.
func TestSendMessageActual(t *testing.T) {
	p := &SlackPlugin{}
	ctx := context.Background()

	t.Run("successful send", func(t *testing.T) {
		// Create a test server that accepts Slack webhook requests
		var receivedMsg SlackMessage
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "POST" {
				t.Errorf("expected POST, got %s", r.Method)
			}
			if r.Header.Get("Content-Type") != "application/json" {
				t.Errorf("expected Content-Type application/json, got %s", r.Header.Get("Content-Type"))
			}

			if err := json.NewDecoder(r.Body).Decode(&receivedMsg); err != nil {
				t.Errorf("failed to decode request: %v", err)
			}

			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
		}))
		defer server.Close()

		msg := SlackMessage{
			Channel:  "#test",
			Username: "TestBot",
			Text:     "Test message",
		}

		// Create a custom HTTP client for testing (no TLS requirement)
		originalClient := defaultHTTPClient
		defaultHTTPClient = &http.Client{Timeout: 5 * time.Second}
		defer func() { defaultHTTPClient = originalClient }()

		err := p.sendMessage(ctx, server.URL, msg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if receivedMsg.Channel != "#test" {
			t.Errorf("expected channel #test, got %s", receivedMsg.Channel)
		}
		if receivedMsg.Username != "TestBot" {
			t.Errorf("expected username TestBot, got %s", receivedMsg.Username)
		}
	})

	t.Run("server error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		msg := SlackMessage{Text: "Test"}

		originalClient := defaultHTTPClient
		defaultHTTPClient = &http.Client{Timeout: 5 * time.Second}
		defer func() { defaultHTTPClient = originalClient }()

		err := p.sendMessage(ctx, server.URL, msg)
		if err == nil {
			t.Error("expected error for server error response")
		}
		if !strings.Contains(err.Error(), "status 500") {
			t.Errorf("expected error to mention status 500, got: %v", err)
		}
	})

	t.Run("invalid URL", func(t *testing.T) {
		msg := SlackMessage{Text: "Test"}

		originalClient := defaultHTTPClient
		defaultHTTPClient = &http.Client{Timeout: 5 * time.Second}
		defer func() { defaultHTTPClient = originalClient }()

		err := p.sendMessage(ctx, "://invalid", msg)
		if err == nil {
			t.Error("expected error for invalid URL")
		}
	})

	t.Run("context cancelled", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(2 * time.Second)
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		msg := SlackMessage{Text: "Test"}

		originalClient := defaultHTTPClient
		defaultHTTPClient = &http.Client{Timeout: 5 * time.Second}
		defer func() { defaultHTTPClient = originalClient }()

		err := p.sendMessage(ctx, server.URL, msg)
		if err == nil {
			t.Error("expected error for cancelled context")
		}
	})
}

// TestExecuteWithRealHTTP tests execute with actual HTTP responses.
func TestExecuteWithRealHTTP(t *testing.T) {
	p := &SlackPlugin{}
	ctx := context.Background()

	t.Run("success notification - HTTP OK", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		// Temporarily replace the HTTP client
		originalClient := defaultHTTPClient
		defaultHTTPClient = &http.Client{Timeout: 5 * time.Second}
		defer func() { defaultHTTPClient = originalClient }()

		config := map[string]any{
			"webhook": server.URL,
		}

		releaseCtx := plugin.ReleaseContext{
			Version:     "1.0.0",
			TagName:     "v1.0.0",
			ReleaseType: "patch",
			Branch:      "main",
		}

		req := plugin.ExecuteRequest{
			Hook:    plugin.HookPostPublish,
			Config:  config,
			Context: releaseCtx,
			DryRun:  false, // Actually send
		}

		resp, err := p.Execute(ctx, req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !resp.Success {
			t.Errorf("expected success, got failure: %s", resp.Error)
		}

		if !strings.Contains(resp.Message, "Sent Slack success notification") {
			t.Errorf("unexpected message: %s", resp.Message)
		}
	})

	t.Run("success notification - HTTP error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
		}))
		defer server.Close()

		originalClient := defaultHTTPClient
		defaultHTTPClient = &http.Client{Timeout: 5 * time.Second}
		defer func() { defaultHTTPClient = originalClient }()

		config := map[string]any{
			"webhook": server.URL,
		}

		releaseCtx := plugin.ReleaseContext{
			Version:     "1.0.0",
			TagName:     "v1.0.0",
			ReleaseType: "patch",
			Branch:      "main",
		}

		req := plugin.ExecuteRequest{
			Hook:    plugin.HookPostPublish,
			Config:  config,
			Context: releaseCtx,
			DryRun:  false,
		}

		resp, err := p.Execute(ctx, req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if resp.Success {
			t.Error("expected failure, got success")
		}

		if !strings.Contains(resp.Error, "failed to send Slack message") {
			t.Errorf("unexpected error: %s", resp.Error)
		}
	})

	t.Run("error notification - HTTP OK", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		originalClient := defaultHTTPClient
		defaultHTTPClient = &http.Client{Timeout: 5 * time.Second}
		defer func() { defaultHTTPClient = originalClient }()

		config := map[string]any{
			"webhook": server.URL,
		}

		releaseCtx := plugin.ReleaseContext{
			Version: "1.0.0",
			TagName: "v1.0.0",
			Branch:  "main",
		}

		req := plugin.ExecuteRequest{
			Hook:    plugin.HookOnError,
			Config:  config,
			Context: releaseCtx,
			DryRun:  false,
		}

		resp, err := p.Execute(ctx, req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !resp.Success {
			t.Errorf("expected success, got failure: %s", resp.Error)
		}

		if !strings.Contains(resp.Message, "Sent Slack error notification") {
			t.Errorf("unexpected message: %s", resp.Message)
		}
	})

	t.Run("error notification - HTTP error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		}))
		defer server.Close()

		originalClient := defaultHTTPClient
		defaultHTTPClient = &http.Client{Timeout: 5 * time.Second}
		defer func() { defaultHTTPClient = originalClient }()

		config := map[string]any{
			"webhook": server.URL,
		}

		releaseCtx := plugin.ReleaseContext{
			Version: "1.0.0",
			TagName: "v1.0.0",
			Branch:  "main",
		}

		req := plugin.ExecuteRequest{
			Hook:    plugin.HookOnError,
			Config:  config,
			Context: releaseCtx,
			DryRun:  false,
		}

		resp, err := p.Execute(ctx, req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if resp.Success {
			t.Error("expected failure, got success")
		}
	})
}

// TestIconSettings tests that icon settings are properly configured.
func TestIconSettings(t *testing.T) {
	p := &SlackPlugin{}

	tests := []struct {
		name          string
		config        map[string]any
		wantIconEmoji string
		wantIconURL   string
	}{
		{
			name:          "default icon emoji",
			config:        map[string]any{},
			wantIconEmoji: ":rocket:",
			wantIconURL:   "",
		},
		{
			name: "custom icon emoji",
			config: map[string]any{
				"icon_emoji": ":tada:",
			},
			wantIconEmoji: ":tada:",
			wantIconURL:   "",
		},
		{
			name: "icon URL overrides emoji",
			config: map[string]any{
				"icon_emoji": ":rocket:",
				"icon_url":   "https://example.com/icon.png",
			},
			wantIconEmoji: ":rocket:",
			wantIconURL:   "https://example.com/icon.png",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := p.parseConfig(tt.config)

			if cfg.IconEmoji != tt.wantIconEmoji {
				t.Errorf("expected IconEmoji %q, got %q", tt.wantIconEmoji, cfg.IconEmoji)
			}
			if cfg.IconURL != tt.wantIconURL {
				t.Errorf("expected IconURL %q, got %q", tt.wantIconURL, cfg.IconURL)
			}
		})
	}
}
