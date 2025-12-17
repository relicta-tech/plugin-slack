# Relicta Slack Plugin

Official Slack plugin for [Relicta](https://github.com/relicta-tech/relicta) - AI-powered release management.

## Features

- Send release notifications to Slack channels
- Rich message formatting with attachments
- Configurable success/error notifications
- User/group mentions support
- Include changelog in notifications

## Installation

```bash
relicta plugin install slack
relicta plugin enable slack
```

## Configuration

Add to your `release.config.yaml`:

```yaml
plugins:
  - name: slack
    enabled: true
    config:
      channel: "#releases"
      username: "Release Bot"
      icon_emoji: ":rocket:"
      notify_on_success: true
      notify_on_error: true
      include_changelog: true
      mentions:
        - "@channel"
```

### Environment Variables

- `SLACK_WEBHOOK_URL` - Slack webhook URL (required)

### Configuration Options

| Option | Description | Default |
|--------|-------------|---------|
| `webhook` | Slack webhook URL (prefer using env var) | - |
| `channel` | Channel to post to | Webhook default |
| `username` | Bot username | `Relicta` |
| `icon_emoji` | Bot icon emoji | `:rocket:` |
| `icon_url` | Bot icon URL | - |
| `notify_on_success` | Send notification on success | `true` |
| `notify_on_error` | Send notification on error | `true` |
| `include_changelog` | Include changelog in message | `false` |
| `mentions` | Users/groups to mention | - |

## Creating a Webhook

1. Go to your Slack workspace
2. Navigate to Apps → Manage → Custom Integrations → Incoming WebHooks
3. Click "Add to Slack"
4. Choose a channel and click "Add Incoming WebHooks integration"
5. Copy the webhook URL and set it as `SLACK_WEBHOOK_URL`

## Hooks

This plugin responds to the following hooks:

- `post_publish` - Sends success notification
- `on_success` - Sends success notification
- `on_error` - Sends error notification

## Development

```bash
# Build
go build -o slack .

# Test locally
relicta plugin install ./slack
relicta plugin enable slack
relicta publish --dry-run
```

## License

MIT License - see [LICENSE](LICENSE) for details.
