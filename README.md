# im2code

Bridge IM messages to tmux terminal sessions. Control your running code directly from Telegram, Discord, Slack, WhatsApp, Feishu, DingTalk, or QQ.

## Supported Platforms

| Platform | Status |
|----------|--------|
| Feishu / Lark | Tested |
| WhatsApp | Tested |
| Telegram | Tested |
| QQ | Untested |
| DingTalk | Untested |
| Slack | Untested |
| Discord | Untested |

---

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

### 3. Activate the bot

Once the daemon is running, send the following from your IM app to claim ownership of the bot:

```
#im2code
```

The bot replies "Activated." and locks to your sender ID, which is automatically saved to `~/.im2code/config.yaml`. All messages from other senders are silently ignored.

### 4. Bind a session from your IM app

Once activated, send these commands in any configured chat:

```
#list              — list tmux sessions
#attach dev        — bind this chat to the "dev" session
#detach            — remove the binding
#status            — show current binding and watch state
```

After binding, every plain message you send is forwarded to the terminal via `tmux send-keys`.

### 5. View terminal output

```
#snap              — capture the current pane (last 50 lines)
#watch on          — push output automatically when the terminal goes idle
#watch off         — stop automatic pushes
#setivl min,max    — adjust watch intervals live (e.g. #setivl 5s,20s)
```

Every plain-text command you send is echoed back as a terminal snapshot ~500ms after it runs, regardless of watch mode.

With `#watch on`, output is pushed automatically:
- **Immediately** when a shell prompt is detected (command finished)
- **Periodically** (every `watchtime_max`) if the terminal changes but no prompt appears
- Suppressed if nothing has changed since the last push

### 6. Send control keys

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
You:  #im2code
Bot:  Activated. Send #help to see available commands.

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

**3. Start the daemon and activate**

```bash
im2code start
```

Send `#im2code` to the bot. Your user ID is automatically saved to `allow_from` in `~/.im2code/config.yaml` and the bot locks to you. All subsequent messages from other users are silently ignored.

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

**3. Activate**

Send `#im2code` in the channel where you want to use the bot. The bot locks to that channel and sender. To pre-restrict to specific channels, set `allow_from` to a list of channel IDs (right-click a channel → Copy ID; requires Developer Mode).

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

**4. Activate**

Send `#im2code` in the channel or DM where you want to use the bot. The bot locks to that sender. To pre-restrict to specific channels or users, set `allow_from` to a list of channel or user IDs.

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

**3. Activate**

Send `#im2code` to the bot's WhatsApp number. The bot locks to your number and saves it to `allow_from`.

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

**4. Activate**

Send `#im2code` as a direct message to the bot. The bot locks to your open_id and saves it to `allow_from`.

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

**3. Activate**

Send `#im2code` as a direct message or @mention in a group. The bot locks to your sender ID and saves it to `allow_from`.

---

### QQ

Uses the [botgo](https://github.com/tencent-connect/botgo) SDK over WebSocket to receive C2C (private) messages.

**1. Create a bot**

- Open the [QQ Bot Open Platform](https://bot.q.qq.com/)
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

**3. Activate**

Send `#im2code` as a private (C2C) message to the bot. The bot locks to your openid and saves it to `allow_from`. Note: QQ Bot uses `openid`, not your QQ number — the openid is shown in the daemon logs on first contact.

---

## Configuration Reference

Config file location: `~/.im2code/config.yaml`

Missing fields are written back with their defaults on every startup, so the file stays self-documenting.

```yaml
# Bridge command prefix. Default: "#"
prefix: "#"

# Log level: debug | info | warn | error. Default: "warn"
loglevel: "warn"

# Log file path. Relative paths resolve to the same directory as the executable.
logfile: "./im2code.log"

# SQLite database for command history (all user inputs with timestamp and sender).
# Default: ~/.im2code/cmd_history.db
cmd_history_db: ""

tmux:
  # How long to wait after a prompt is detected before pushing output
  idle_timeout: "2s"
  # Maximum lines pushed per watch event
  max_output_lines: 50
  # Regexes that match a shell prompt (signals command completion)
  prompt_patterns:
    - '[$#>]\s*$'   # bash / zsh / sh
    - '>>>\s*$'     # Python REPL
  # Minimum interval between automatic watch pushes (1s–3600s). Default: "5s"
  watchtime_min: "5s"
  # Periodic push interval when terminal is idle (1s–3600s). Default: "20s"
  watchtime_max: "20s"

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
#im2code               activate the bot and lock it to your sender ID (first use)
#list                  list tmux sessions
#attach <session>      bind this chat to a session
#detach                remove the binding
#status                show current session and watch state
#snap                  capture the current pane
#watch on|off          enable / disable automatic output push
#setivl min,max        set watch intervals (e.g. 5s,20s); no args prints current
#key <key>             send a control key (e.g. ctrl-c, ctrl-d, esc, Enter, Tab)
#help                  show available commands
```

---

## Data Directory

```
~/.im2code/
├── config.yaml          configuration (defaults written on first run)
├── subscriptions.json   session bindings (managed automatically)
├── cmd_history.db       SQLite log of all user inputs
└── whatsapp/            WhatsApp pairing data
```
