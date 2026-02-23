# im2code

Bridge IM messages to tmux terminal sessions. Control your running code directly from Telegram, Discord, Slack, WhatsApp, Feishu, DingTalk, or QQ.

## How It Works

```
IM message → im2code → tmux send-keys → terminal
tmux output → im2code → IM message
```

im2code runs as a daemon, forwarding incoming IM messages to a bound tmux session. When the terminal goes idle (a shell prompt is detected), output is automatically pushed back to the chat.

---

## Installation

### Build from Source

Requires Go 1.25+.

```bash
git clone https://github.com/dfbb/im2code
cd im2code
go build -o im2code ./cmd/im2code
sudo mv im2code /usr/local/bin/
```

Verify:

```bash
im2code version
```

---

## tmux Integration

### 1. Start a named tmux session

im2code works with existing tmux sessions — it does not create them. Use meaningful names:

```bash
tmux new-session -s dev
tmux new-session -s prod
```

### 2. Start the daemon

```bash
im2code start
```

The daemon reads `~/.im2code/config.yaml` and enables every channel that has credentials configured.

Enable specific channels only:

```bash
im2code start --channels telegram,slack
```

Use a different command prefix (default `#`):

```bash
im2code start --prefix "!"
```

### 3. Bind a session from your IM app

Once the daemon is running, send these commands in any configured chat:

```
#list              — list tmux sessions
#attach dev        — bind this chat to the "dev" session
#detach            — remove the binding
#status            — show current binding and watch state
```

After binding, every plain message you send is forwarded to the terminal via `tmux send-keys`.

### 4. View terminal output

```
#snap              — capture the current pane (last 50 lines)
#watch on          — push output automatically when the terminal goes idle
#watch off         — stop automatic pushes
```

With `#watch on`, output is sent back to the chat each time a shell prompt is detected (i.e., the running command has finished).

### 5. Send control keys

```
#key ctrl-c        — interrupt (SIGINT)
#key ctrl-d        — EOF
#key ctrl-z        — suspend
#key Enter         — carriage return
#key Tab           — tab / autocomplete
#key esc           — Escape
```

Both `ctrl-x` and `ctrl+x` are accepted as separators.

### Typical workflow

```
You:  #list
Bot:  Sessions:
        dev
        prod

You:  #attach dev
Bot:  Attached to session: dev

You:  ls -la
      (terminal runs ls -la)

You:  #watch on
Bot:  Watch mode enabled.

You:  npm test
      (terminal runs tests; when done, output is pushed back)
Bot:  ```
      PASS src/app.test.ts
      Tests: 12 passed
      ```
```

---

## IM Platform Setup

### Telegram

**1. Create a bot**

Open [@BotFather](https://t.me/BotFather) in Telegram, send `/newbot`, and follow the prompts. You'll receive a Bot Token in the format `123456789:AAxxxxxx`.

**2. Configure**

```bash
im2code login telegram
```

**3. Start the daemon and send a message**

```bash
im2code start
```

Send any message to the bot. The first sender's user ID is automatically saved to `allow_from` in `~/.im2code/config.yaml`, and all subsequent messages from other users are ignored. No manual configuration needed.

To allow additional users, add their IDs to `allow_from` in the config file manually.

---

### Discord

**1. Create a bot**

- Open the [Discord Developer Portal](https://discord.com/developers/applications)
- **New Application** → give it a name
- Left sidebar → **Bot** → **Reset Token** to get the token
- On the **Bot** page, enable **Message Content Intent** (required)
- **OAuth2 → URL Generator**: select the `bot` scope, add `Send Messages` and `Read Message History` permissions, then invite the bot with the generated URL

**2. Configure**

```bash
im2code login discord
```

**3. Restrict channels**

Set `allow_from` to a list of channel IDs (right-click a channel → Copy ID; requires Developer Mode to be enabled).

---

### Slack

**1. Create an app**

- Open [Slack API](https://api.slack.com/apps) → **Create New App** → **From scratch**
- **OAuth & Permissions → Bot Token Scopes**: add `chat:write`, `channels:history`, `im:history`
- **Event Subscriptions**: enable and subscribe to `message.channels`, `message.im`
- **Socket Mode**: enable (this is how the App Token is used)
- **Install App** to your workspace to get the `xoxb-` Bot Token

**2. Get an App Token**

- **Basic Information → App-Level Tokens** → **Generate Token and Scopes**
- Add the `connections:write` scope; the generated token starts with `xapp-`

**3. Configure**

```bash
im2code login slack
# Bot Token (xoxb-...):
# App Token (xapp-...):
```

**4. Restrict channels**

Set `allow_from` to a list of channel or user IDs.

---

### WhatsApp

WhatsApp uses QR-code pairing — no bot token required.

**1. First run**

```bash
im2code start --channels whatsapp
```

On first launch an ASCII QR code is printed to the terminal. Scan it with your phone:

**WhatsApp** → **Settings** → **Linked Devices** → **Link a Device**

**2. Persistent session**

Pairing data is stored in `~/.im2code/whatsapp/` (configurable). Subsequent restarts reconnect automatically without re-scanning.

---

### Feishu / Lark

**1. Create an app**

- Open the [Feishu Open Platform](https://open.feishu.cn/app)
- **Create an in-house app**
- Go to **Credentials & Basic Info** and copy the **App ID** and **App Secret**
- **Permissions**: add `im:message` (receive) and `im:message:send_as_bot` (send)

**2. Configure and establish first connection**

```bash
im2code login feishu
# App ID: cli_xxxxxx
# App Secret:
```

This verifies your credentials, saves them, and automatically opens a brief WebSocket connection to Feishu. That first connection is required by Feishu before the long-connection option becomes available in the console.

**3. Enable long-connection mode**

After `im2code login feishu` completes, go back to the Feishu developer console:

- **Event Subscriptions** → enable **Long Connection** mode
- Subscribe to the `im.message.receive_v1` event

**4. Usage**

Send a direct message to the bot, or add it to a group and @mention it.

---

### DingTalk

**1. Create a bot app**

- Open the [DingTalk Open Platform](https://open.dingtalk.com/)
- **App Development → In-house App → Bot**
- After creation, go to **App Credentials** and copy the **Client ID** and **Client Secret**
- **Message Push**: enable Stream mode

**2. Configure**

```bash
im2code login dingtalk
# Client ID: ding_xxxxxx
# Client Secret:
```

**3. Usage**

Send a direct message to the bot, or @mention it in a group.

---

### QQ

Uses the [botgo](https://github.com/tencent-connect/botgo) SDK over WebSocket to receive C2C (private) messages.

**1. Create a bot**

- Open the [QQ Bot Open Platform](https://q.qq.com/qqbot)
- Register a developer account and create a bot app
- Go to **Development → Basic Info** and copy the **AppID** and **AppSecret** (used as `secret`)
- **Feature Config**: enable C2C (single-chat) message permission (`C2C_MESSAGE_CREATE` event)
- Add yourself to the sandbox member list (only sandbox members can use the bot before it goes live)

**2. Configure**

```bash
im2code login qq
# App ID: 123456789
# Secret:
```

**3. User identifiers**

QQ Bot identifies users by `openid`, not QQ number. The openid is printed in the logs when a user first messages the bot. Add openids to `allow_from` to restrict access.

---

## Configuration Reference

Config file location: `~/.im2code/config.yaml`

```yaml
# Bridge command prefix. Default: "#"
prefix: "#"

tmux:
  # How long to wait after a prompt is detected before pushing output
  idle_timeout: "2s"
  # Maximum lines pushed per watch event
  max_output_lines: 50
  # Regexes that match a shell prompt (signals command completion)
  prompt_patterns:
    - '[$#>]\s*$'   # bash / zsh / sh
    - '>>>\s*$'     # Python REPL

channels:
  telegram:
    token: "123456789:AAxxxxxx"
    allow_from:           # empty = accept everyone
      - "your_chat_id"

  discord:
    token: "Bot xxxxxxxx"
    allow_from:           # empty = accept all channels
      - "channel_id_1"

  slack:
    bot_token: "xoxb-xxxxxxx"
    app_token: "xapp-xxxxxxx"
    allow_from:           # empty = accept all
      - "channel_id"

  whatsapp:
    session_dir: "~/.im2code/whatsapp"

  feishu:
    app_id: "cli_xxxxxx"
    app_secret: "xxxxxxxx"

  dingtalk:
    client_id: "ding_xxxxxx"
    client_secret: "xxxxxxxx"

  qq:
    app_id: "xxxxxxxx"
    secret: "xxxxxxxx"
    allow_from: []        # empty = accept all (list of openids)
```

---

## Command Reference

### CLI

```
im2code start               Start the daemon
  --config <path>           Config file path (default: ~/.im2code/config.yaml)
  --prefix <str>            Override command prefix
  --channels <list>         Enable only these channels, e.g. telegram,slack

im2code login <channel>     Configure credentials for a channel
  channel: telegram | discord | slack | whatsapp | feishu | dingtalk | qq

im2code check               Verify credentials for all configured channels

im2code version             Print version
```

### In-chat bridge commands (default prefix `#`)

```
#list              list tmux sessions
#attach <session>  bind this chat to a session
#detach            remove the binding
#status            show current session and watch state
#snap              capture the current pane
#watch on|off      enable / disable automatic output push
#key <key>         send a control key (e.g. ctrl-c, ctrl-d, esc, Enter, Tab)
#help              show available commands
```

---

## Data Directory

```
~/.im2code/
├── config.yaml          configuration
├── subscriptions.json   session bindings (managed automatically)
└── whatsapp/            WhatsApp pairing data
```
