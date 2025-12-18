// Package main implements the Slack plugin for Relicta.
package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"

	"github.com/relicta-tech/relicta-plugin-sdk/helpers"
	"github.com/relicta-tech/relicta-plugin-sdk/plugin"
)

// Shared HTTP client for connection reuse across requests.
// Includes security hardening: TLS 1.3+, redirect protection, SSRF prevention.
var defaultHTTPClient = &http.Client{
	Timeout: 10 * time.Second,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		// Limit redirect chain length
		if len(via) >= 3 {
			return fmt.Errorf("too many redirects")
		}
		// Prevent redirect to non-HTTPS
		if req.URL.Scheme != "https" {
			return fmt.Errorf("redirect to non-HTTPS URL not allowed")
		}
		// Prevent redirect away from hooks.slack.com (SSRF protection)
		if req.URL.Host != "hooks.slack.com" {
			return fmt.Errorf("redirect away from hooks.slack.com not allowed")
		}
		return nil
	},
	Transport: &http.Transport{
		MaxIdleConns:        10,
		MaxIdleConnsPerHost: 5,
		IdleConnTimeout:     90 * time.Second,
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS13,
		},
	},
}

// SlackPlugin implements the Slack notification plugin.
type SlackPlugin struct{}

// Config represents the Slack plugin configuration.
type Config struct {
	// WebhookURL is the Slack webhook URL.
	WebhookURL string `json:"webhook,omitempty"`
	// Channel is the channel to post to (overrides webhook default).
	Channel string `json:"channel,omitempty"`
	// Username is the bot username.
	Username string `json:"username,omitempty"`
	// IconEmoji is the bot icon emoji.
	IconEmoji string `json:"icon_emoji,omitempty"`
	// IconURL is the bot icon URL.
	IconURL string `json:"icon_url,omitempty"`
	// NotifyOnSuccess sends notification on successful release.
	NotifyOnSuccess bool `json:"notify_on_success"`
	// NotifyOnError sends notification on failed release.
	NotifyOnError bool `json:"notify_on_error"`
	// IncludeChangelog includes changelog in the notification.
	IncludeChangelog bool `json:"include_changelog"`
	// Mentions is a list of users/groups to mention.
	Mentions []string `json:"mentions,omitempty"`
}

// SlackMessage represents a Slack message payload.
type SlackMessage struct {
	Channel     string        `json:"channel,omitempty"`
	Username    string        `json:"username,omitempty"`
	IconEmoji   string        `json:"icon_emoji,omitempty"`
	IconURL     string        `json:"icon_url,omitempty"`
	Text        string        `json:"text,omitempty"`
	Attachments []Attachment  `json:"attachments,omitempty"`
	Blocks      []interface{} `json:"blocks,omitempty"`
}

// Attachment represents a Slack attachment.
type Attachment struct {
	Color      string  `json:"color,omitempty"`
	Title      string  `json:"title,omitempty"`
	TitleLink  string  `json:"title_link,omitempty"`
	Text       string  `json:"text,omitempty"`
	Fields     []Field `json:"fields,omitempty"`
	Footer     string  `json:"footer,omitempty"`
	FooterIcon string  `json:"footer_icon,omitempty"`
	Ts         int64   `json:"ts,omitempty"`
}

// Field represents a field in a Slack attachment.
type Field struct {
	Title string `json:"title"`
	Value string `json:"value"`
	Short bool   `json:"short"`
}

// GetInfo returns plugin metadata.
func (p *SlackPlugin) GetInfo() plugin.Info {
	return plugin.Info{
		Name:        "slack",
		Version:     "2.0.0",
		Description: "Send Slack notifications for releases",
		Author:      "Relicta Team",
		Hooks: []plugin.Hook{
			plugin.HookPostPublish,
			plugin.HookOnSuccess,
			plugin.HookOnError,
		},
		ConfigSchema: `{
			"type": "object",
			"properties": {
				"webhook": {"type": "string", "description": "Slack webhook URL (or use SLACK_WEBHOOK_URL env)"},
				"channel": {"type": "string", "description": "Channel to post to"},
				"username": {"type": "string", "description": "Bot username", "default": "Relicta"},
				"icon_emoji": {"type": "string", "description": "Bot icon emoji", "default": ":rocket:"},
				"icon_url": {"type": "string", "description": "Bot icon URL"},
				"notify_on_success": {"type": "boolean", "description": "Notify on success", "default": true},
				"notify_on_error": {"type": "boolean", "description": "Notify on error", "default": true},
				"include_changelog": {"type": "boolean", "description": "Include changelog", "default": false},
				"mentions": {"type": "array", "items": {"type": "string"}, "description": "Users/groups to mention"}
			},
			"required": ["webhook"]
		}`,
	}
}

// Execute runs the plugin for a given hook.
func (p *SlackPlugin) Execute(ctx context.Context, req plugin.ExecuteRequest) (*plugin.ExecuteResponse, error) {
	cfg := p.parseConfig(req.Config)

	switch req.Hook {
	case plugin.HookPostPublish, plugin.HookOnSuccess:
		if !cfg.NotifyOnSuccess {
			return &plugin.ExecuteResponse{
				Success: true,
				Message: "Success notification disabled",
			}, nil
		}
		return p.sendSuccessNotification(ctx, cfg, req.Context, req.DryRun)

	case plugin.HookOnError:
		if !cfg.NotifyOnError {
			return &plugin.ExecuteResponse{
				Success: true,
				Message: "Error notification disabled",
			}, nil
		}
		return p.sendErrorNotification(ctx, cfg, req.Context, req.DryRun)

	default:
		return &plugin.ExecuteResponse{
			Success: true,
			Message: fmt.Sprintf("Hook %s not handled", req.Hook),
		}, nil
	}
}

// buildSlackMentions formats mentions for Slack.
func buildSlackMentions(mentions []string) string {
	if len(mentions) == 0 {
		return ""
	}
	// Slack mentions are like <@U123456> for users or <!subteam^S123456> for groups
	var formatted []string
	for _, m := range mentions {
		if strings.HasPrefix(m, "<@") || strings.HasPrefix(m, "<!") {
			formatted = append(formatted, m)
		} else if strings.HasPrefix(m, "@") {
			formatted = append(formatted, fmt.Sprintf("<@%s>", strings.TrimPrefix(m, "@")))
		} else {
			formatted = append(formatted, fmt.Sprintf("<@%s>", m))
		}
	}
	return strings.Join(formatted, " ")
}

// sendSuccessNotification sends a success notification.
func (p *SlackPlugin) sendSuccessNotification(ctx context.Context, cfg *Config, releaseCtx plugin.ReleaseContext, dryRun bool) (*plugin.ExecuteResponse, error) {
	// Build message
	title := fmt.Sprintf(":rocket: Release %s Published!", releaseCtx.Version)

	fields := []Field{
		{Title: "Version", Value: releaseCtx.Version, Short: true},
		{Title: "Release Type", Value: cases.Title(language.English).String(releaseCtx.ReleaseType), Short: true},
		{Title: "Branch", Value: releaseCtx.Branch, Short: true},
		{Title: "Tag", Value: releaseCtx.TagName, Short: true},
	}

	if releaseCtx.Changes != nil {
		features := len(releaseCtx.Changes.Features)
		fixes := len(releaseCtx.Changes.Fixes)
		breaking := len(releaseCtx.Changes.Breaking)

		summary := fmt.Sprintf("%d features, %d fixes", features, fixes)
		if breaking > 0 {
			summary += fmt.Sprintf(", %d breaking changes", breaking)
		}
		fields = append(fields, Field{Title: "Changes", Value: summary, Short: false})
	}

	text := ""
	if cfg.IncludeChangelog && releaseCtx.ReleaseNotes != "" {
		// Truncate if too long
		notes := releaseCtx.ReleaseNotes
		if len(notes) > 2000 {
			notes = notes[:2000] + "..."
		}
		// Escape HTML to prevent XSS attacks in release notes
		text = html.EscapeString(notes)
	}

	// Add mentions
	mentionText := buildSlackMentions(cfg.Mentions)

	msg := SlackMessage{
		Channel:   cfg.Channel,
		Username:  cfg.Username,
		IconEmoji: cfg.IconEmoji,
		IconURL:   cfg.IconURL,
		Text:      mentionText,
		Attachments: []Attachment{
			{
				Color:  "good",
				Title:  title,
				Text:   text,
				Fields: fields,
				Footer: "Relicta",
				Ts:     time.Now().Unix(),
			},
		},
	}

	if dryRun {
		return &plugin.ExecuteResponse{
			Success: true,
			Message: "Would send Slack success notification",
			Outputs: map[string]any{
				"channel": cfg.Channel,
				"version": releaseCtx.Version,
			},
		}, nil
	}

	if err := p.sendMessage(ctx, cfg.WebhookURL, msg); err != nil {
		return &plugin.ExecuteResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to send Slack message: %v", err),
		}, nil
	}

	return &plugin.ExecuteResponse{
		Success: true,
		Message: "Sent Slack success notification",
	}, nil
}

// sendErrorNotification sends an error notification.
func (p *SlackPlugin) sendErrorNotification(ctx context.Context, cfg *Config, releaseCtx plugin.ReleaseContext, dryRun bool) (*plugin.ExecuteResponse, error) {
	title := fmt.Sprintf(":x: Release %s Failed", releaseCtx.Version)

	fields := []Field{
		{Title: "Version", Value: releaseCtx.Version, Short: true},
		{Title: "Branch", Value: releaseCtx.Branch, Short: true},
	}

	// Add mentions
	mentionText := buildSlackMentions(cfg.Mentions)

	msg := SlackMessage{
		Channel:   cfg.Channel,
		Username:  cfg.Username,
		IconEmoji: cfg.IconEmoji,
		IconURL:   cfg.IconURL,
		Text:      mentionText,
		Attachments: []Attachment{
			{
				Color:  "danger",
				Title:  title,
				Fields: fields,
				Footer: "Relicta",
				Ts:     time.Now().Unix(),
			},
		},
	}

	if dryRun {
		return &plugin.ExecuteResponse{
			Success: true,
			Message: "Would send Slack error notification",
		}, nil
	}

	if err := p.sendMessage(ctx, cfg.WebhookURL, msg); err != nil {
		return &plugin.ExecuteResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to send Slack message: %v", err),
		}, nil
	}

	return &plugin.ExecuteResponse{
		Success: true,
		Message: "Sent Slack error notification",
	}, nil
}

// sendMessage sends a message to Slack.
func (p *SlackPlugin) sendMessage(ctx context.Context, webhookURL string, msg SlackMessage) error {
	payload, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", webhookURL, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := defaultHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("slack returned status %d", resp.StatusCode)
	}

	return nil
}

// parseConfig parses the plugin configuration.
func (p *SlackPlugin) parseConfig(raw map[string]any) *Config {
	parser := helpers.NewConfigParser(raw)

	// Get webhook URL with env fallback
	webhook := parser.GetString("webhook", "SLACK_WEBHOOK_URL", "")

	return &Config{
		WebhookURL:       webhook,
		Channel:          parser.GetString("channel", "", ""),
		Username:         parser.GetString("username", "", "Relicta"),
		IconEmoji:        parser.GetString("icon_emoji", "", ":rocket:"),
		IconURL:          parser.GetString("icon_url", "", ""),
		NotifyOnSuccess:  parser.GetBool("notify_on_success", true),
		NotifyOnError:    parser.GetBool("notify_on_error", true),
		IncludeChangelog: parser.GetBool("include_changelog", false),
		Mentions:         parser.GetStringSlice("mentions", nil),
	}
}

// validateSlackWebhookURL validates a Slack webhook URL.
func validateSlackWebhookURL(webhookURL string) error {
	if webhookURL == "" {
		return fmt.Errorf("webhook URL is required")
	}

	parsed, err := url.Parse(webhookURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	if parsed.Scheme != "https" {
		return fmt.Errorf("webhook URL must use HTTPS")
	}

	if parsed.Host != "hooks.slack.com" {
		return fmt.Errorf("webhook URL must be on hooks.slack.com")
	}

	if !strings.HasPrefix(parsed.Path, "/services/") {
		return fmt.Errorf("webhook URL path must start with /services/")
	}

	return nil
}

// Validate validates the plugin configuration.
func (p *SlackPlugin) Validate(_ context.Context, config map[string]any) (*plugin.ValidateResponse, error) {
	vb := helpers.NewValidationBuilder()

	// Get webhook URL with env fallback
	parser := helpers.NewConfigParser(config)
	webhook := parser.GetString("webhook", "SLACK_WEBHOOK_URL", "")

	// Check environment fallback if not in config
	if webhook == "" {
		webhook = os.Getenv("SLACK_WEBHOOK_URL")
	}

	if webhook == "" {
		vb.AddErrorWithCode("webhook",
			"Slack webhook URL is required (set SLACK_WEBHOOK_URL env var or configure webhook)",
			"required")
	} else {
		if err := validateSlackWebhookURL(webhook); err != nil {
			vb.AddErrorWithCode("webhook", err.Error(), "format")
		}
	}

	return vb.Build(), nil
}
