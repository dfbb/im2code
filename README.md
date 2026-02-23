# im2code

将 IM 消息桥接到 tmux 终端会话。在 Telegram、Discord、Slack、WhatsApp、飞书、钉钉中直接控制你的代码运行环境。

## 工作原理

```
IM 消息 → im2code → tmux send-keys → 终端
tmux 输出 → im2code → IM 消息
```

im2code 作为守护进程运行，监听 IM 消息并将其转发到指定的 tmux 会话；当终端空闲时（检测到提示符），自动将输出发回 IM。

---

## 安装

### 从源码构建

需要 Go 1.25+。

```bash
git clone https://github.com/dfbb/im2code
cd im2code
go build -o im2code ./cmd/im2code
sudo mv im2code /usr/local/bin/
```

验证安装：

```bash
im2code version
```

---

## tmux 集成

### 1. 启动或接入 tmux 会话

im2code 操作已有的 tmux 会话，不创建新会话。建议给会话起有意义的名字：

```bash
# 创建命名会话
tmux new-session -s dev

# 或在已有 tmux 中新建命名窗口
tmux new-session -s prod
```

### 2. 启动守护进程

```bash
im2code start
```

守护进程会读取 `~/.im2code/config.yaml`，并启用其中配置了凭证的所有频道。

只启用特定频道：

```bash
im2code start --channels telegram,slack
```

使用不同的命令前缀（默认 `#`）：

```bash
im2code start --prefix "!"
```

### 3. 在 IM 中绑定会话

守护进程启动后，在 IM 中发送以下命令：

```
#list              — 列出所有 tmux 会话
#attach dev        — 将此对话绑定到 dev 会话
#detach            — 解除绑定
#status            — 查看当前绑定状态
```

绑定后，向 IM 发送的所有普通消息都会通过 `tmux send-keys` 发送到终端。

### 4. 查看终端输出

```
#snap              — 截取当前终端内容（最近 50 行）
#watch on          — 开启实时推送（终端空闲时自动发回输出）
#watch off         — 关闭实时推送
```

`#watch on` 模式下，每当终端检测到 shell 提示符（命令执行完成），输出会自动发回 IM。

### 5. 发送控制键

```
#key ctrl-c        — 发送 Ctrl-C（中断）
#key ctrl-d        — 发送 Ctrl-D（EOF）
#key Enter         — 发送回车
#key Tab           — 发送 Tab（补全）
#key Escape        — 发送 Esc
```

### 典型工作流

```
你：#list
机器人：Sessions:
          dev
          prod

你：#attach dev
机器人：Attached to session: dev

你：ls -la
（终端执行 ls -la）

你：#watch on
机器人：Watch mode enabled.

你：npm test
（终端执行，测试完成后输出自动发回）
机器人：```
         PASS src/app.test.ts
         Tests: 12 passed
         ```
```

---

## IM 平台接入

### Telegram

**1. 创建机器人**

在 Telegram 中找 [@BotFather](https://t.me/BotFather)，发送 `/newbot`，按提示操作，获得 Bot Token（格式：`123456789:AAxxxxxx`）。

**2. 配置**

```bash
im2code login telegram
# 输入 Bot Token
```

**3. 获取你的 Chat ID**

启动守护进程后，向机器人发送任意消息，日志中会打印 Chat ID。或使用 `@userinfobot` 查询。

**4. 限制允许发言的用户（推荐）**

在配置文件中设置 `allow_from`（见下方配置参考），填入你的 Chat ID 或用户名。未在列表中的用户会被忽略。

---

### Discord

**1. 创建机器人**

- 打开 [Discord Developer Portal](https://discord.com/developers/applications)
- 点击 **New Application** → 填写名称
- 左侧选 **Bot** → **Reset Token** 获取 Token
- 在 **Bot** 页面开启 **Message Content Intent**（必须）
- 在 **OAuth2 → URL Generator** 中勾选 `bot` scope 和 `Send Messages`、`Read Message History` 权限，用生成的链接邀请机器人入服务器

**2. 配置**

```bash
im2code login discord
# 输入 Bot Token
```

**3. 限制频道**

在配置文件中设置 `allow_from`，填入允许交互的频道 ID（右键频道 → 复制 ID，需开启开发者模式）。

---

### Slack

**1. 创建 App**

- 打开 [Slack API](https://api.slack.com/apps) → **Create New App** → **From scratch**
- **OAuth & Permissions → Bot Token Scopes** 添加：`chat:write`、`channels:history`、`im:history`
- **Event Subscriptions** 开启，订阅 `message.channels`、`message.im`
- **Socket Mode** 开启（用于 App Token）
- **Install App** 安装到工作区，获取 `xoxb-` 开头的 Bot Token

**2. 获取 App Token**

- **Basic Information → App-Level Tokens** → **Generate Token and Scopes**
- Scope 选 `connections:write`，生成 `xapp-` 开头的 App Token

**3. 配置**

```bash
im2code login slack
# Bot Token (xoxb-...):
# App Token (xapp-...):
```

**4. 限制频道**

在配置文件中设置 `allow_from`，填入允许交互的频道 ID 或用户 ID。

---

### WhatsApp

WhatsApp 使用 QR 码配对，无需 Bot Token。

**1. 首次启动**

```bash
im2code start --channels whatsapp
```

启动后，终端（stderr）会打印一个 QR 码字符串，将其粘贴到任意在线 QR 生成器（如 qr-code-generator.com），用手机 WhatsApp 扫码：

- 手机 WhatsApp → **设置 → 已关联的设备 → 关联设备** → 扫码

**2. 会话持久化**

配对信息存储在 `~/.im2code/whatsapp/`（可通过配置修改）。之后重启无需重新扫码。

**3. 使用方式**

配对后，在 WhatsApp 中向机器人账号发消息即可，使用方式与其他平台相同。

---

### 飞书（Feishu / Lark）

**1. 创建应用**

- 打开[飞书开放平台](https://open.feishu.cn/app)
- **创建企业自建应用**
- 进入应用 → **凭证与基础信息**，记录 **App ID** 和 **App Secret**
- **权限管理** → 添加权限：`im:message`（接收消息）、`im:message:send_as_bot`（发送消息）
- **事件订阅** → 开启长连接，订阅 `im.message.receive_v1`

**2. 配置**

```bash
im2code login feishu
# App ID: cli_xxxxxx
# App Secret: （输入后不回显）
```

**3. 使用方式**

在飞书中向机器人发消息，或将机器人拉入群组后 @机器人 发消息。

---

### 钉钉（DingTalk）

**1. 创建机器人应用**

- 打开[钉钉开放平台](https://open.dingtalk.com/)
- **应用开发 → 企业内部应用 → 机器人**
- 创建应用后，进入 **应用凭证**，记录 **Client ID** 和 **Client Secret**
- **消息推送** → 开启 Stream 模式

**2. 配置**

```bash
im2code login dingtalk
# Client ID: ding_xxxxxx
# Client Secret: （输入后不回显）
```

**3. 使用方式**

在钉钉中向机器人发消息，或在群组中 @机器人。

---

### QQ

使用 [botgo](https://github.com/tencent-connect/botgo) SDK，通过 WebSocket 接收 C2C（用户私信）消息。

**1. 创建机器人**

- 打开 [QQ 开放平台](https://q.qq.com/qqbot)
- 注册开发者账号，创建机器人应用
- 进入应用 → **开发 → 基本信息**，记录 **AppID** 和 **AppSecret**（即 Secret）
- **功能配置** → 开启"单聊"消息权限（C2C_MESSAGE_CREATE 事件）
- 将机器人添加到沙盒成员列表（正式上线前只有沙盒成员可使用）

**2. 配置**

```bash
im2code login qq
# App ID: 123456789
# Secret: （输入后不回显）
```

**3. 用户标识**

QQ Bot 使用 `openid`（不是 QQ 号）标识用户。用户向机器人发私信后，日志中会打印对应 openid。在 `allow_from` 中填入允许的 openid 可限制访问。

**4. 使用方式**

在 QQ 中向机器人发私信，使用方式与其他平台相同。

---

## 完整配置参考

配置文件路径：`~/.im2code/config.yaml`

```yaml
# 桥接命令前缀，默认 "#"
prefix: "#"

tmux:
  # 终端空闲判定超时（检测到提示符后等待此时间确认空闲）
  idle_timeout: "2s"
  # watch 模式每次最多推送的行数
  max_output_lines: 50
  # shell 提示符正则，用于判断命令是否执行完成
  prompt_patterns:
    - '[$#>]\s*$'   # bash/zsh/sh
    - '>>>\s*$'      # Python REPL

channels:
  telegram:
    token: "123456789:AAxxxxxx"
    allow_from:        # 留空则接受所有用户
      - "your_chat_id"

  discord:
    token: "Bot xxxxxxxx"
    allow_from:        # 留空则接受所有频道
      - "channel_id_1"

  slack:
    bot_token: "xoxb-xxxxxxx"
    app_token: "xapp-xxxxxxx"
    allow_from:        # 留空则接受所有
      - "channel_id"

  whatsapp:
    session_dir: "~/.im2code/whatsapp"   # 配对信息存储目录

  feishu:
    app_id: "cli_xxxxxx"
    app_secret: "xxxxxxxx"

  dingtalk:
    client_id: "ding_xxxxxx"
    client_secret: "xxxxxxxx"

  qq:
    app_id: "xxxxxxxx"
    secret: "xxxxxxxx"
    allow_from: []          # 留空则接受所有用户（openid 列表）
```

---

## 命令参考

### im2code CLI

```
im2code start               启动守护进程
  --config <path>           指定配置文件路径
  --prefix <str>            覆盖命令前缀
  --channels <list>         只启用指定频道，如 telegram,slack

im2code login <channel>     配置指定频道的凭证
  channel: telegram | discord | slack | whatsapp | feishu | dingtalk | qq

im2code check               检查所有已配置频道的 Token 是否有效

im2code version             显示版本号
```

### IM 内桥接命令（默认前缀 `#`）

```
#list              列出所有 tmux 会话
#attach <session>  将此对话绑定到指定会话
#detach            解除绑定
#status            查看当前绑定和 watch 状态
#snap              截取当前终端内容
#watch on|off      开启/关闭实时推送
#key <key>         发送控制键（如 ctrl-c、ctrl-d、Enter、Tab、Escape）
#help              显示帮助
```

---

## 数据目录

```
~/.im2code/
├── config.yaml          配置文件
├── subscriptions.json   会话绑定状态（自动维护）
└── whatsapp/            WhatsApp 配对数据
```
