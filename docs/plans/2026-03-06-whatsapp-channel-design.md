# WhatsApp Bidirectional Channel — Design Document

**Date:** 2026-03-06
**Status:** Approved

## 1. Overview

Foreman gains a bidirectional WhatsApp channel for ticket submission and lifecycle notifications. Users send messages from their personal WhatsApp to create tickets, respond to clarifications, query status, and control the daemon. Foreman replies with status updates, clarification questions, and PR links.

WhatsApp is implemented as a **Channel** — a new abstraction separate from `IssueTracker`. Tickets are created directly in Foreman's database (like the local file tracker). No tickets are mirrored to GitHub, Jira, or Linear.

**Library:** `go.mau.fi/whatsmeow` (Go-native, WhatsApp Web multi-device protocol)
**Dependency:** Personal WhatsApp number — no Business API, no third-party service

## 2. Architecture Decision: Channel, Not Tracker

The `IssueTracker` interface is pull-based — the daemon polls `FetchReadyTickets()`. WhatsApp is push-based — messages arrive asynchronously via a persistent websocket. Forcing WhatsApp into `IssueTracker` would mean half the methods are no-ops or semantically wrong (`AddLabel`, `RemoveLabel`, `HasLabel` are meaningless in WhatsApp).

Instead, WhatsApp implements a new `Channel` interface — a transport-only abstraction for inbound/outbound text messaging. The `ChannelRouter` handles classification and routing. The orchestrator calls `channel.Send()` at lifecycle transitions for notifications.

```
WhatsApp (Personal Number)
     |  WhatsApp Web Multi-Device Protocol
     v
internal/channel/whatsapp/     -- whatsmeow client, session, reconnect
     |
     v
internal/channel/router.go    -- allowlist, classify, route
     |
     +-- new ticket ---------> DB (tickets table, status=queued)
     +-- clarification reply -> DB (update ticket, requeue)
     +-- command ------------> daemon (pause/resume/status/cost)
     |
daemon poll loop               -- picks up queued tickets (unchanged)
     |
     v
orchestrator.ProcessTicket()   -- notify() calls channel.Send() at transitions
```

## 3. Channel Interface

```go
// internal/channel/channel.go

type Channel interface {
    // Start begins listening for inbound messages.
    // Blocks until ctx is cancelled or a fatal error occurs.
    // Must be called in a goroutine by the caller.
    Start(ctx context.Context, handler InboundHandler) error

    // Stop disconnects the channel transport. Called by daemon on shutdown
    // or when the channel reports an unrecoverable error (e.g., session revoked).
    Stop() error

    // Send sends a text message to a recipient.
    Send(ctx context.Context, recipientID string, message string) error

    // Name returns the channel name (e.g., "whatsapp").
    Name() string
}

type InboundMessage struct {
    SenderID  string
    Body      string
    Timestamp time.Time
}

type InboundHandler interface {
    HandleMessage(ctx context.Context, msg InboundMessage) error
}
```

## 4. ChannelRouter

```go
// internal/channel/router.go

type ChannelRouter struct {
    channel    Channel
    db         db.Database
    classifier Classifier
    allowlist  *Allowlist
    pairing    *PairingManager
    commands   CommandHandler
    logger     zerolog.Logger
}
```

### HandleMessage Flow

```
InboundMessage
  |
  +-- allowlist.IsAllowed(senderID)?
  |    no --> pairing.HandleUnknown(senderID, msg) -> send pairing code, discard
  |
  +-- db.FindActiveClarification(ctx, senderID) returns *models.Ticket?
  |    yes --> handle clarification reply, requeue ticket
  |
  +-- classifier.Classify(ctx, body) -> MessageKind
  |
  +-- KindCommand(cmd)
  |    /status  -> commands.Status() -> channel.Send(summary)
  |    /pause   -> commands.Pause()  -> channel.Send(confirmation)
  |    /resume  -> commands.Resume() -> channel.Send(confirmation)
  |    /cost    -> commands.Cost()   -> channel.Send(summary)
  |
  +-- KindNewTicket
       db.CreateTicket(title=first line, body, status=queued, channel_sender_id)
       -> channel.Send("Ticket #42 created -- planning now...")
```

### Classifier

```go
// internal/channel/classifier.go

type MessageKind struct {
    Kind    string // "command" | "new_ticket"
    Command string // e.g., "status", "pause" -- only if Kind == "command"
}

type Classifier struct {
    llm llm.LlmProvider // fallback only
}

func (c *Classifier) Classify(ctx context.Context, body string) MessageKind
```

Classification logic (sequential, short-circuits):
1. **Prefix match**: starts with `/status`, `/pause`, `/resume`, `/cost` -> `KindCommand`
2. **LLM fallback**: for ambiguous messages like "what's going on" or "stop" -> classify via clarifier model
3. **Default**: `KindNewTicket`

Note: Clarification replies are handled by the router before the classifier is called (via `FindActiveClarification` DB check). The classifier is pure text — no DB dependency, no sender context.

### CommandHandler

```go
type CommandHandler interface {
    Status(ctx context.Context) (string, error)
    Pause(ctx context.Context) (string, error)
    Resume(ctx context.Context) (string, error)
    Cost(ctx context.Context) (string, error)
}
```

The daemon implements this interface. The router doesn't know about daemon internals.

## 5. WhatsApp Implementation

```go
// internal/channel/whatsapp/whatsapp.go

type WhatsAppChannel struct {
    client      *whatsmeow.Client
    device      *store.Device
    handler     channel.InboundHandler
    logger      zerolog.Logger
    rateLimiter *rateLimiter
    sessionDB   string // path to ~/.foreman/whatsapp.db
    mu          sync.Mutex
    connected   bool
}
```

### Start (blocking)

1. Open/create session SQLite DB at `sessionDB` path
2. Load existing device from store, or wait for login (pairing code / QR)
3. Connect whatsmeow client
4. Register event handlers:
   - `events.Message` -> filter and dispatch to `handler.HandleMessage()`
   - `events.LoggedOut` -> log warning, `go w.Stop()` (async to avoid deadlock)
   - `events.Disconnected` -> reconnect with context-aware backoff
5. Block on `<-ctx.Done()`
6. Cleanup: disconnect client, close session DB

### Inbound Message Filtering

Explicit sequential order in the event handler:

```go
func (w *WhatsAppChannel) handleMessage(ctx context.Context, evt *events.Message) {
    if evt.Info.IsGroup            { return }                          // 1. groups
    if evt.Info.IsFromMe           { return }                          // 2. echo
    if isMediaMessage(evt.Message) {                                   // 3. media
        w.Send(ctx, evt.Info.Sender.String(), "Attachments not supported -- please describe in text")
        return
    }
    if !w.rateLimiter.Allow(evt.Info.Sender.String()) { return }      // 4. rate limit
    body := extractBody(evt.Message)
    if len(body) > 2000 { body = body[:2000] }                        // 5. truncate
    w.handler.HandleMessage(ctx, channel.InboundMessage{
        SenderID:  evt.Info.Sender.String(),
        Body:      body,
        Timestamp: evt.Info.Timestamp,
    })
}
```

Order matters: group check first (cheapest), media check before rate limit (media senders don't burn quota).

### Reconnect (context-aware)

```go
func (w *WhatsAppChannel) handleDisconnect(ctx context.Context) {
    backoff := []time.Duration{5*time.Second, 10*time.Second, 30*time.Second, 60*time.Second}
    for attempt := 0; ; attempt++ {
        delay := backoff[min(attempt, len(backoff)-1)]
        select {
        case <-ctx.Done():
            w.logger.Info().Msg("reconnect cancelled -- shutting down")
            return
        case <-time.After(delay):
        }
        if err := w.client.Connect(); err == nil {
            w.logger.Info().Msg("reconnected")
            return
        }
    }
}
```

### Rate Limiter

Simple windowed counter per JID with TTL eviction:

```go
type rateLimiter struct {
    mu      sync.Mutex
    buckets map[string]*rateBucket
}

type rateBucket struct {
    count     int
    windowEnd time.Time
}
```

- Max 10 messages per sender per minute
- Cleanup ticker in `Start()` deletes expired buckets every 5 minutes

### Session Login

Two modes, used only by `foreman channel login`:

- **Pairing code** (default): `client.PairPhone(phone, ...)`, prints code to terminal, blocks until linked
- **QR code** (fallback): listens for `events.QR`, renders via `qrterminal`, blocks until scanned

After first login, session persists in `sessionDB`. Subsequent `Start()` calls are fully headless.

## 6. Security: Allowlist & Pairing

### Allowlist

Only numbers in `allowed_numbers` can submit tickets. All other senders are rejected or challenged.

### Pairing Flow (dm_policy = "pairing")

When an unknown sender messages:
1. Generate random 8-char alphanumeric code
2. Persist to `pending_pairings` DB table with sender JID + 10-minute TTL
3. Reply: "Pairing code: XKCD-7291 -- Run: foreman pairing approve XKCD-7291"
4. Discard the triggering message (no ticket created)
5. On `foreman pairing approve <CODE>`: add sender to `allowed_numbers` in `foreman.toml`, delete pairing row
6. On TTL expiry: deleted by `DeleteExpiredPairings()` in daemon poll loop

Rate limit: max 3 pairing attempts per unknown sender per hour.

### Abuse Protection

- Rate limit: max 10 messages per sender per minute
- Max ticket body: 2000 characters (truncate and warn)
- Media messages: rejected with friendly reply
- Group messages: silently ignored

## 7. Daemon & Orchestrator Integration

### Ticket Schema Addition

```sql
ALTER TABLE tickets ADD COLUMN channel_sender_id TEXT DEFAULT '';
```

### Pairing Schema

```sql
CREATE TABLE IF NOT EXISTS pending_pairings (
    code        TEXT PRIMARY KEY,
    sender_id   TEXT NOT NULL,
    channel     TEXT NOT NULL DEFAULT 'whatsapp',
    expires_at  DATETIME NOT NULL,
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

### Daemon Changes

```go
type Daemon struct {
    // ... existing fields ...
    channel       channel.Channel       // optional, nil if not configured
    channelRouter *channel.ChannelRouter // optional, set via SetChannelRouter()
}
```

- `Start()`: launches `channel.Start(ctx, router)` in a goroutine
- Shutdown: `channel.Stop()` after context cancel, before drain wait
- Implements `CommandHandler` (Status/Pause/Resume/Cost)
- Poll loop: calls `db.DeleteExpiredPairings(ctx)` alongside existing clarification timeout check

Circular dependency resolved via setter:
```go
dmn := daemon.NewDaemon(..., ch)       // router not yet created
router := channel.NewRouter(ch, db, classifier, allowlist, pairingMgr, dmn, logger)
dmn.SetChannelRouter(router)           // wire back after construction
```

### Orchestrator Changes

```go
type Orchestrator struct {
    // ... existing fields ...
    channel channel.Channel // optional, nil if no channel configured
}

func (o *Orchestrator) notify(ctx context.Context, ticket models.Ticket, msg string) {
    if o.channel == nil || ticket.ChannelSenderID == "" {
        return
    }
    if err := o.channel.Send(ctx, ticket.ChannelSenderID, msg); err != nil {
        o.logger.Warn().Err(err).Str("ticket", ticket.ID).Msg("channel notify failed")
    }
}
```

Notification points (fire-and-forget, never blocks ticket processing):

| Transition | Message |
|---|---|
| queued -> planning | Ticket #42 picked up -- planning... |
| -> clarification_needed | [question from planner] |
| -> implementing | Implementing N tasks... |
| -> pr_created | PR #87: https://github.com/... |
| -> failed | Failed: [reason] |
| Cost limit hit | Daily cost limit reached. Daemon paused. |

### Shutdown Sequence

```
1. Cancel context         -> poll loop exits, no new tickets
2. channel.Stop()         -> disconnect WhatsApp (stop inbound)
3. WaitForDrain(timeout)  -> active ProcessTicket goroutines finish
```

Channel stopped before drain so in-flight `notify()` calls during drain return error (logged, not fatal).

## 8. Configuration

### foreman.toml

```toml
[channel]
provider = "whatsapp"

[channel.whatsapp]
session_db       = "~/.foreman/whatsapp.db"
pairing_mode     = "code"              # code | qr
dm_policy        = "pairing"           # pairing | reject
allowed_numbers  = ["+84xxxxxxxxx"]    # E.164 format
```

### Config Structs

```go
type ChannelConfig struct {
    Provider string                `mapstructure:"provider"` // "" (disabled) | "whatsapp"
    WhatsApp WhatsAppChannelConfig `mapstructure:"whatsapp"`
}

type WhatsAppChannelConfig struct {
    SessionDB      string   `mapstructure:"session_db"`
    PairingMode    string   `mapstructure:"pairing_mode"`
    DMPolicy       string   `mapstructure:"dm_policy"`
    AllowedNumbers []string `mapstructure:"allowed_numbers"`
}
```

## 9. CLI Commands

```
foreman channel login --phone +84xxxxxxxxx [--mode code|qr]
    Links a WhatsApp account. Run once per installation. Does NOT start daemon.

foreman channel status
    Shows connection state, phone number, session age.

foreman pairing list
    Shows pending pairing codes with sender and expiry.

foreman pairing approve <CODE>
    Approves pairing, adds sender to allowed_numbers in foreman.toml.

foreman pairing revoke <PHONE>
    Removes number from allowed_numbers in foreman.toml.
```

Config write-back uses `pelletier/go-toml` v1 for round-trip TOML editing that preserves comments.

## 10. DB Interface Additions

```go
type Database interface {
    // ... existing methods ...

    // Pairing
    CreatePairing(ctx context.Context, code, senderID, channel string, expiresAt time.Time) error
    GetPairing(ctx context.Context, code string) (*models.Pairing, error)
    DeletePairing(ctx context.Context, code string) error
    ListPairings(ctx context.Context, channel string) ([]models.Pairing, error)
    DeleteExpiredPairings(ctx context.Context) error

    // Channel queries
    FindActiveClarification(ctx context.Context, senderID string) (*models.Ticket, error)
}
```

## 11. File Inventory

### New Files

| File | Responsibility | ~Lines |
|---|---|---|
| `internal/channel/channel.go` | `Channel`, `InboundHandler`, `InboundMessage` interfaces/types | ~30 |
| `internal/channel/router.go` | `ChannelRouter` -- allowlist, classify, route | ~120 |
| `internal/channel/classifier.go` | `Classifier` -- prefix match + LLM fallback | ~60 |
| `internal/channel/pairing.go` | `PairingManager` -- code gen, DB persist, approve/revoke | ~80 |
| `internal/channel/whatsapp/whatsapp.go` | `WhatsAppChannel` -- whatsmeow client, connect, reconnect | ~150 |
| `internal/channel/whatsapp/session.go` | Login flows -- pairing code + QR | ~60 |
| `internal/config/persist.go` | `AddAllowedNumber`, `RemoveAllowedNumber` -- round-trip TOML | ~40 |
| `internal/models/pairing.go` | `Pairing` model struct | ~20 |
| `cmd/channel.go` | `foreman channel login`, `foreman channel status` | ~60 |
| `cmd/pairing.go` | `foreman pairing list/approve/revoke` | ~70 |

### Modified Files

| File | Change |
|---|---|
| `internal/models/config.go` | Add `ChannelConfig`, `WhatsAppChannelConfig` |
| `internal/models/ticket.go` | Add `ChannelSenderID` field |
| `internal/db/db.go` | Add pairing + clarification query methods |
| `internal/db/schema.go` | Add `channel_sender_id` column, `pending_pairings` table |
| `internal/db/sqlite.go` | Implement new DB methods |
| `internal/db/postgres.go` | Implement new DB methods |
| `internal/daemon/daemon.go` | Add `channel` field, `SetChannelRouter()`, `CommandHandler` impl, channel start/stop, expired pairings cleanup |
| `internal/daemon/orchestrator.go` | Add `channel` field, `notify()` method, calls at 6 transition points |
| `cmd/start.go` | Wire channel + router if configured |
| `cmd/root.go` | Register `channel` and `pairing` subcommands |

### Dependencies (go.mod)

```
go.mau.fi/whatsmeow              # WhatsApp Web multi-device protocol
go.mau.fi/util                    # whatsmeow utility dependency
github.com/pelletier/go-toml v1   # round-trip TOML editing (v1, not v2)
github.com/mdp/qrterminal/v3     # QR code rendering in terminal
google.golang.org/protobuf        # verify if already present before adding
```

**Estimated total: ~690 new lines, ~100 lines modified across existing files.**

Zero changes to: tracker interface, pipeline, planner, implementer, reviewers, git operations, runner, dashboard, skills engine.
