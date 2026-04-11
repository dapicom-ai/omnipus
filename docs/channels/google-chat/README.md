> Back to [README](../../../README.md)

# Google Chat

Omnipus supports Google Chat via two modes: **webhook** (outbound only, simple setup) and **bot** (full interactive, receives and sends messages).

## Configuration

```json
{
  "channels": {
    "google-chat": {
      "enabled": true,
      "mode": "webhook",
      "webhook_url": "https://chat.googleapis.com/webhook/123456",
      "space": "spaces/abc123",
      "allow_from": []
    }
  }
}
```

### Bot Mode

```json
{
  "channels": {
    "google-chat": {
      "enabled": true,
      "mode": "bot",
      "service_account_json": "-----BEGIN SERVICE_ACCOUNT JSON-----\n{...}\n-----END SERVICE_ACCOUNT JSON-----",
      "space": "spaces/abc123",
      "allow_from": []
    }
  }
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| enabled | bool | Yes | Whether to enable the Google Chat channel |
| mode | string | Yes | `"webhook"` (outbound only) or `"bot"` (full interactive) |
| webhook_url | string | Yes (webhook) | Incoming webhook URL from Google Chat |
| service_account_json | string | Yes (bot) | Inline service account JSON (encrypted) |
| service_account_file | string | Yes (bot) | Path to service account JSON file |
| space | string | No | Google Chat space name for display |
| bot_user | string | No | Bot user email for identification |
| allow_from | array | No | User email whitelist; empty means all users are allowed |
| group_trigger | object | No | Group chat trigger configuration |
| reasoning_channel_id | string | No | Channel ID for reasoning responses |

## Setup

### Webhook Mode

1. Go to the Google Chat space where you want to post messages
2. Click **...** > **Configure webhooks**
3. Give the webhook a name and copy the webhook URL
4. Set `webhook_url` in your Omnipus configuration

### Bot Mode

1. Create a Google Cloud project and enable the Google Chat API
2. Create a service account and download the JSON key file
3. Configure the Chat API:
   - Set the bot name and avatar
   - Add the service account email to the Google Chat space
   - Enable bot interactions
4. Set `service_account_json` (or `service_account_file`) in your Omnipus configuration

## Group Trigger

The `group_trigger` section controls when the bot responds in group spaces:

```json
{
  "group_trigger": {
    "mention_only": false,
    "prefixes": ["/ask", "/omnipus"]
  }
}
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| mention_only | bool | false | Only respond when @mentioned |
| prefixes | array | [] | Commands that trigger response (e.g., `/ask`) |

When `mention_only` is `true`, the bot only responds when directly @mentioned.
When `prefixes` are set, the bot responds to messages starting with any prefix.
If neither is set, the bot responds to all messages (permissive default for DMs).

## Features

- **Webhook mode**: Post messages to Google Chat spaces via incoming webhooks
- **Bot mode**: Full bidirectional messaging with JWT authentication
- **Signature verification**: Bot mode verifies inbound webhook signatures using RSA keys from Google's JWKS endpoint
- **Typing indicators**: Bot mode sends typing indicators while composing responses
- **Thread support**: Replies are sent as thread responses when a thread key is present
