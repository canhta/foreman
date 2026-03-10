# Phase 3: Chat & Notifications — Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a chat interface for agent-to-user communication in the ticket detail view, modify the pipeline's clarification logic to write to `chat_messages`, and configure WhatsApp as a notification forwarder.

**Architecture:** The `chat_messages` table and API endpoints already exist from Phase 1b. The `projectState` already has `chatMessages` and `sendChatMessage`. This phase builds the UI components and wires the pipeline to use chat instead of tracker-only clarification.

**Tech Stack:** Svelte 5, TypeScript, Go (pipeline modifications)

**Spec:** `docs/superpowers/specs/2026-03-10-multi-project-refactor-design.md` (Section 8)

**Prerequisites:** Phase 1b (chat_messages table, chat API endpoints) and Phase 2 (frontend redesign with TicketPanel/TicketFullView) must be completed.

---

## File Structure

```
Frontend:
src/components/
  ChatInterface.svelte              # Chat message list + input
  TicketFullView.svelte              # Full page ticket view (updated with chat)
  TicketPanel.svelte                 # Side panel (updated with chat preview)

Backend:
internal/pipeline/
  clarification.go                  # Modified to write to chat_messages
internal/channel/
  whatsapp.go                       # Modified to forward notifications with dashboard link
```

---

## Chunk 1: Chat UI Components

### Task 1: Build ChatInterface Component

**Files:**
- Create: `internal/dashboard/web/src/components/ChatInterface.svelte`

- [ ] **Step 1: Create chat interface component**

Create `src/components/ChatInterface.svelte`:

```svelte
<script lang="ts">
  import type { ChatMessage } from '../types';

  interface Props {
    messages: ChatMessage[];
    onSend: (content: string) => void;
    disabled?: boolean;
  }
  let { messages, onSend, disabled = false }: Props = $props();

  let input = $state('');
  let container: HTMLDivElement;

  function send() {
    const content = input.trim();
    if (!content) return;
    onSend(content);
    input = '';
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      send();
    }
  }

  function senderLabel(sender: string): string {
    switch (sender) {
      case 'agent': return 'AGENT';
      case 'user': return 'YOU';
      case 'system': return 'SYSTEM';
      default: return sender.toUpperCase();
    }
  }

  function senderColor(sender: string): string {
    switch (sender) {
      case 'agent': return 'text-[var(--color-accent)]';
      case 'user': return 'text-[var(--color-success)]';
      case 'system': return 'text-[var(--color-muted)]';
      default: return 'text-[var(--color-text)]';
    }
  }

  function typeIndicator(type: string): string {
    switch (type) {
      case 'clarification': return '?';
      case 'action_request': return '!';
      case 'error': return '✕';
      default: return '';
    }
  }

  function typeColor(type: string): string {
    switch (type) {
      case 'clarification': return 'text-[var(--color-warning)]';
      case 'action_request': return 'text-[var(--color-warning)]';
      case 'error': return 'text-[var(--color-danger)]';
      default: return '';
    }
  }

  // Auto-scroll to bottom on new messages
  $effect(() => {
    if (messages.length && container) {
      container.scrollTop = container.scrollHeight;
    }
  });
</script>

<div class="flex flex-col h-full">
  <!-- Messages -->
  <div bind:this={container} class="flex-1 overflow-y-auto p-3 space-y-3">
    {#if messages.length === 0}
      <div class="text-xs text-[var(--color-muted)] text-center py-8">
        No messages yet. Agent communication will appear here.
      </div>
    {/if}

    {#each messages as msg (msg.id)}
      <div class="text-xs animate-fade-in p-2 -mx-2 border-l-2 transition-colors"
           class:border-transparent={msg.message_type === 'info' || msg.message_type === 'reply'}
           class:border-[var(--color-warning)]={msg.message_type === 'clarification' || msg.message_type === 'action_request'}
           class:border-[var(--color-danger)]={msg.message_type === 'error'}
           class:bg-[var(--color-warning-bg)]={msg.message_type === 'clarification' || msg.message_type === 'action_request'}
           class:bg-[var(--color-danger-bg)]={msg.message_type === 'error'}>
        <div class="flex items-center gap-2 mb-0.5">
          <span class={`text-[10px] tracking-widest font-bold ${senderColor(msg.sender)}`}>
            {senderLabel(msg.sender)}
          </span>
          {#if typeIndicator(msg.message_type)}
            <span class={`text-[10px] ${typeColor(msg.message_type)}`}>
              {typeIndicator(msg.message_type)} {msg.message_type.replace(/_/g, ' ')}
            </span>
          {/if}
          <span class="text-[10px] text-[var(--color-muted)]">
            {new Date(msg.created_at).toLocaleTimeString()}
          </span>
        </div>
        <div
          class="whitespace-pre-wrap leading-relaxed"
          class:text-[var(--color-text)]={msg.sender === 'agent'}
          class:text-[var(--color-muted-bright)]={msg.sender === 'user'}
          class:text-[var(--color-muted)]={msg.sender === 'system'}
        >
          {msg.content}
        </div>

        <!-- Structured action request with options -->
        {#if msg.message_type === 'action_request' && msg.metadata}
          {@const meta = JSON.parse(msg.metadata)}
          {#if meta.options}
            <div class="mt-2 flex gap-2">
              {#each meta.options as option}
                <button
                  onclick={() => onSend(option)}
                  class="text-[10px] px-2 py-1 border border-[var(--color-border)] text-[var(--color-accent)] hover:bg-[var(--color-accent-bg)]"
                >
                  {option}
                </button>
              {/each}
            </div>
          {/if}
        {/if}
      </div>
    {/each}
  </div>

  <!-- Input -->
  <div class="border-t border-[var(--color-border)] p-3">
    <div class="flex gap-2">
      <textarea
        bind:value={input}
        onkeydown={handleKeydown}
        {disabled}
        rows="1"
        placeholder={disabled ? 'Chat disabled' : 'Type a reply...'}
        class="flex-1 bg-[var(--color-surface)] border border-[var(--color-border)] px-3 py-2 text-xs text-[var(--color-text)] placeholder-[var(--color-muted)] focus:border-[var(--color-accent)] focus:outline-none disabled:opacity-50 resize-none min-h-[36px] max-h-24"
        oninput={(e) => { e.target.style.height = 'auto'; e.target.style.height = e.target.scrollHeight + 'px'; }}
      ></textarea>
      <button
        onclick={send}
        {disabled}
        class="px-3 py-2 bg-[var(--color-accent)] text-[var(--color-bg)] text-[10px] font-bold tracking-widest disabled:opacity-50"
      >
        SEND
      </button>
    </div>
  </div>
</div>
```

- [ ] **Step 2: Verify build**

```bash
cd internal/dashboard/web && npm run build
```
Expected: SUCCESS

- [ ] **Step 3: Commit**

```bash
git add internal/dashboard/web/src/components/ChatInterface.svelte
git commit -m "feat(ui): add ChatInterface component for agent communication"
```

---

### Task 2: Build TicketFullView (Full-Page Detail with Chat)

**Files:**
- Create: `internal/dashboard/web/src/components/TicketFullView.svelte`

- [ ] **Step 1: Create full-page ticket view**

Create `src/components/TicketFullView.svelte`:

```svelte
<script lang="ts">
  import { projectState } from '../state/project.svelte';
  import ChatInterface from './ChatInterface.svelte';
  import TaskCard from './TaskCard.svelte';
  import ActivityStream from './ActivityStream.svelte';
  import CostBreakdown from './CostBreakdown.svelte';

  function statusLabel(s: string): string { return s.replace(/_/g, ' ').toUpperCase(); }
</script>

{#if projectState.ticketDetail}
  {@const ticket = projectState.ticketDetail}
  <div class="h-full flex flex-col animate-fade-in">
    <!-- Header bar -->
    <div class="flex items-center justify-between px-6 py-3 border-b border-[var(--color-border)]">
      <div class="flex items-center gap-3">
        <button
          onclick={() => projectState.panelExpanded = false}
          class="text-[var(--color-muted)] hover:text-[var(--color-text)] text-sm"
        >◂ Back</button>
        <span class="text-xs text-[var(--color-muted)]">{ticket.external_id || ticket.id.slice(0, 8)}</span>
        <span class="text-xs font-bold">{ticket.title}</span>
        <span class="text-[10px] text-[var(--color-accent)]">{statusLabel(ticket.status)}</span>
      </div>
      <div class="flex items-center gap-2">
        {#if ticket.pr_url}
          <a href={ticket.pr_url} target="_blank" rel="noopener"
             class="text-[10px] px-2 py-1 border border-[var(--color-border)] text-[var(--color-accent)] hover:bg-[var(--color-accent-bg)]">
            PR #{ticket.pr_number} →
          </a>
        {/if}
        <span class="text-[10px] text-[var(--color-muted)]">${ticket.cost_usd?.toFixed(2) ?? '0.00'}</span>
      </div>
    </div>

    <!-- Two-column layout -->
    <div class="flex-1 flex overflow-hidden">
      <!-- Left column (60%) — ticket content -->
      <div class="w-3/5 overflow-y-auto p-6 border-r border-[var(--color-border)]">
        <!-- Description -->
        {#if ticket.description}
          <div class="mb-6">
            <div class="text-[10px] tracking-widest text-[var(--color-muted)] uppercase mb-2">Description</div>
            <div class="text-xs text-[var(--color-muted-bright)] whitespace-pre-wrap">{ticket.description}</div>
          </div>
        {/if}

        <!-- Tasks -->
        <div class="mb-6">
          <div class="text-[10px] tracking-widest text-[var(--color-muted)] uppercase mb-2">
            Tasks ({projectState.ticketTasks.filter(t => t.status === 'done').length}/{projectState.ticketTasks.length})
          </div>
          <div class="space-y-2">
            {#each projectState.ticketTasks as task (task.id)}
              <TaskCard {task} />
            {/each}
          </div>
        </div>

        <!-- Cost breakdown -->
        {#if projectState.ticketLlmCalls.length > 0}
          <div class="mb-6">
            <div class="text-[10px] tracking-widest text-[var(--color-muted)] uppercase mb-2">Cost Breakdown</div>
            <CostBreakdown ticket={ticket} llmCalls={projectState.ticketLlmCalls} />
          </div>
        {/if}

        <!-- Activity -->
        <div>
          <div class="text-[10px] tracking-widest text-[var(--color-muted)] uppercase mb-2">Activity</div>
          <ActivityStream events={projectState.ticketEvents} tasks={projectState.ticketTasks} />
        </div>
      </div>

      <!-- Right column (40%) — chat -->
      <div class="w-2/5 flex flex-col">
        <div class="px-4 py-2 border-b border-[var(--color-border)]">
          <span class="text-[10px] tracking-widest text-[var(--color-muted)] uppercase">Chat</span>
        </div>
        <div class="flex-1">
          <ChatInterface
            messages={projectState.chatMessages}
            onSend={(content) => projectState.sendChatMessage(ticket.id, content)}
          />
        </div>
      </div>
    </div>
  </div>
{/if}
```

- [ ] **Step 2: Wire into ProjectBoard**

In `src/pages/ProjectBoard.svelte`, replace the placeholder full-page section:

```svelte
{#if projectState.panelExpanded && projectState.selectedTicketId}
  <div class="fixed inset-0 z-50 bg-[var(--color-bg)] animate-[zoom-in_0.15s_ease-out]">
    <TicketFullView />
  </div>
{/if}
```

Add import: `import TicketFullView from '../components/TicketFullView.svelte';`

- [ ] **Step 3: Verify build**

```bash
cd internal/dashboard/web && npm run build
```
Expected: SUCCESS

- [ ] **Step 4: Commit**

```bash
git add internal/dashboard/web/src/components/TicketFullView.svelte internal/dashboard/web/src/pages/ProjectBoard.svelte
git commit -m "feat(ui): add full-page ticket view with chat interface"
```

---

### Task 3: Add Chat Preview to Side Panel

**Files:**
- Modify: `internal/dashboard/web/src/components/TicketPanel.svelte`

- [ ] **Step 1: Add recent chat messages to side panel**

In `src/components/TicketPanel.svelte`, after the Tasks section, add a chat preview:

```svelte
<!-- Chat preview (last 3 messages) -->
{#if projectState.chatMessages.length > 0}
  <div class="mt-4">
    <div class="text-[10px] tracking-widest text-[var(--color-muted)] uppercase mb-2">Recent Chat</div>
    {#each projectState.chatMessages.slice(-3) as msg (msg.id)}
      <div class="text-xs mb-2">
        <span class="text-[10px] font-bold" class:text-[var(--color-accent)]={msg.sender === 'agent'} class:text-[var(--color-success)]={msg.sender === 'user'}>
          {msg.sender === 'agent' ? 'AGENT' : 'YOU'}:
        </span>
        <span class="text-[var(--color-muted-bright)]">{msg.content.slice(0, 100)}{msg.content.length > 100 ? '...' : ''}</span>
      </div>
    {/each}
    {#if projectState.chatMessages.length > 3}
      <button
        onclick={() => projectState.panelExpanded = true}
        class="text-[10px] text-[var(--color-accent)] hover:underline"
      >
        View all ({projectState.chatMessages.length} messages) →
      </button>
    {/if}
  </div>
{/if}
```

- [ ] **Step 2: Verify build and commit**

```bash
cd internal/dashboard/web && npm run build
git add internal/dashboard/web/src/components/TicketPanel.svelte
git commit -m "feat(ui): add chat preview to ticket side panel"
```

---

## Chunk 2: Pipeline Integration

### Task 4: Modify Pipeline Clarification to Write to chat_messages

**Files:**
- Modify: `internal/pipeline/clarification.go` (or wherever clarification messages are created)
- Modify: `internal/daemon/orchestrator.go` (if clarification is handled there)

- [ ] **Step 1: Find the current clarification logic**

Search for where the pipeline creates clarification requests — likely in `internal/pipeline/` where `ClarityChecker` is implemented or where `clarification_pending` status is set. Read the relevant files.

- [ ] **Step 2: Add chat_messages write alongside existing clarification**

When the pipeline sets a ticket to `clarification_pending`, also write a `chat_message`:

```go
// After setting ticket status to clarification_pending:
chatMsg := &models.ChatMessage{
    ID:          uuid.New().String(),
    TicketID:    ticket.ID,
    Sender:      "agent",
    MessageType: "clarification",
    Content:     clarificationQuestion,
    Metadata:    "", // JSON with task context if available
}
if err := db.CreateChatMessage(ctx, chatMsg); err != nil {
    log.Warn().Err(err).Msg("failed to write chat message for clarification")
}
```

- [ ] **Step 3: Add chat_messages write for action requests**

When agents encounter blockers (test failures needing guidance, confirmation needed), write to chat_messages:

```go
chatMsg := &models.ChatMessage{
    ID:          uuid.New().String(),
    TicketID:    ticket.ID,
    Sender:      "agent",
    MessageType: "action_request",
    Content:     "Tests failed 2 times. Should I try a different approach or skip this task?",
    Metadata:    `{"options": ["Try different approach", "Skip task", "Retry same approach"]}`,
}
```

- [ ] **Step 4: Read user replies from chat_messages**

When the pipeline checks for clarification responses (currently reads from tracker), also check `chat_messages` for `sender=user, message_type=reply`:

```go
messages, err := db.GetChatMessages(ctx, ticket.ID, 100)
if err == nil {
    for _, msg := range messages {
        if msg.Sender == "user" && msg.MessageType == "reply" && msg.CreatedAt.After(clarificationRequestedAt) {
            // User has replied — use this as the clarification response
            return msg.Content, nil
        }
    }
}
```

- [ ] **Step 5: Run pipeline tests**

```bash
go test ./internal/pipeline/... -v -count=1
```
Expected: All tests pass

- [ ] **Step 6: Commit**

```bash
git add internal/pipeline/ internal/daemon/
git commit -m "feat: write clarification and action requests to chat_messages"
```

---

### Task 5: Configure WhatsApp as Notification Forwarder

**Files:**
- Modify: `internal/channel/whatsapp.go` (or the channel handler)

- [ ] **Step 1: Find current WhatsApp message handling**

Read `internal/channel/` to understand how WhatsApp messages are currently sent/received for clarifications.

- [ ] **Step 2: Modify outbound messages to include dashboard link**

When the channel sends a clarification notification, include the dashboard URL:

```go
// Instead of sending the full question via WhatsApp:
dashboardURL := fmt.Sprintf("http://%s:%d/#/projects/%s/board?ticket=%s",
    cfg.Dashboard.Host, cfg.Dashboard.Port, projectID, ticket.ID)

message := fmt.Sprintf(
    "%s / %s: Agent needs input\n\n%s\n\nRespond in dashboard: %s",
    projectName, ticket.ExternalID, shortQuestion, dashboardURL,
)
```

- [ ] **Step 3: Route inbound WhatsApp replies to the correct project**

If a user replies via WhatsApp, the channel handler needs to:
1. Identify which project/ticket the reply is for (from the conversation context)
2. Write a `chat_message` with `sender=user, message_type=reply`
3. Update the ticket status from `clarification_pending` if applicable

- [ ] **Step 4: Run channel tests**

```bash
go test ./internal/channel/... -v -count=1
```
Expected: All tests pass

- [ ] **Step 5: Commit**

```bash
git add internal/channel/
git commit -m "feat: configure WhatsApp as notification forwarder with dashboard links"
```

---

## Chunk 3: Final Verification

### Task 6: End-to-End Verification

- [ ] **Step 1: Full backend build**

```bash
go build ./...
```
Expected: SUCCESS

- [ ] **Step 2: Full frontend build**

```bash
cd internal/dashboard/web && npm run build
```
Expected: SUCCESS

- [ ] **Step 3: Full test suite**

```bash
go test ./... -short -count=1
```
Expected: All tests pass

- [ ] **Step 4: Manual integration test**

1. Start daemon with a project configured
2. Create a ticket that triggers clarification (low confidence plan)
3. Verify: chat_message appears in project DB
4. Verify: dashboard shows chat message in ticket side panel
5. Verify: expanding to full view shows chat interface
6. Verify: typing a reply creates a user message
7. Verify: WhatsApp notification received (if configured) with dashboard link
8. Verify: agent resumes after user reply

- [ ] **Step 5: Commit any remaining fixes**

```bash
git add -A
git commit -m "fix: address issues from end-to-end verification"
```
