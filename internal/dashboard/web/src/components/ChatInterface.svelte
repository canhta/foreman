<script lang="ts">
  import type { ChatMessage } from '../types';

  interface Props {
    messages: ChatMessage[];
    onSend: (content: string) => void;
    disabled?: boolean;
  }

  let { messages, onSend, disabled = false }: Props = $props();

  let inputValue = $state('');
  let messagesContainer: HTMLElement;
  let textareaEl: HTMLTextAreaElement;

  $effect(() => {
    // Auto-scroll to bottom when messages change
    if (messagesContainer && messages.length) {
      messagesContainer.scrollTop = messagesContainer.scrollHeight;
    }
  });

  function senderLabel(sender: ChatMessage['sender']): string {
    if (sender === 'agent') return 'AGENT';
    if (sender === 'user') return 'YOU';
    return 'SYSTEM';
  }

  function senderColor(sender: ChatMessage['sender']): string {
    if (sender === 'agent') return 'text-[var(--color-accent)]';
    if (sender === 'user') return 'text-[var(--color-success)]';
    return 'text-[var(--color-muted)]';
  }

  function messageTypeLabel(type: ChatMessage['message_type']): string {
    if (type === 'clarification') return 'CLARIFICATION';
    if (type === 'action_request') return 'ACTION REQUIRED';
    if (type === 'error') return 'ERROR';
    return '';
  }

  function messageTypeChipClass(type: ChatMessage['message_type']): string {
    if (type === 'clarification' || type === 'action_request') return 'status-chip status-chip-warn';
    if (type === 'error') return 'status-chip status-chip-failed';
    return '';
  }

  function messageContainerClass(msg: ChatMessage): string {
    if (msg.sender === 'user') {
      return 'border-l-[3px] border-l-[var(--color-success)] bg-[var(--color-accent-bg)]';
    }
    if (msg.message_type === 'clarification' || msg.message_type === 'action_request') {
      return 'border-l-[3px] border-l-[var(--color-warning)] bg-[var(--color-warning-bg,rgba(255,184,0,0.05))]';
    }
    if (msg.message_type === 'error') {
      return 'border-l-[3px] border-l-[var(--color-danger)] bg-[var(--color-danger-bg,rgba(255,50,50,0.05))]';
    }
    return '';
  }

  function parseOptions(msg: ChatMessage): string[] {
    if (msg.message_type !== 'action_request' || !msg.metadata) return [];
    try {
      const parsed = JSON.parse(msg.metadata);
      return Array.isArray(parsed?.options) ? parsed.options : [];
    } catch {
      return [];
    }
  }

  function formatTime(isoString: string): string {
    try {
      return new Date(isoString).toLocaleTimeString();
    } catch {
      return '';
    }
  }

  function handleSend() {
    const content = inputValue.trim();
    if (!content || disabled) return;
    onSend(content);
    inputValue = '';
    if (textareaEl) {
      textareaEl.style.height = 'auto';
    }
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      handleSend();
    }
  }

  function handleInput() {
    if (!textareaEl) return;
    textareaEl.style.height = 'auto';
    const maxHeight = 24 * 6; // ~6 lines
    textareaEl.style.height = Math.min(textareaEl.scrollHeight, maxHeight) + 'px';
  }
</script>

<div class="flex flex-col h-full">
  <!-- Message list -->
  <div
    bind:this={messagesContainer}
    class="flex-1 overflow-y-auto divide-y divide-[var(--color-border)]"
    role="log"
    aria-label="Chat messages"
  >
    {#if messages.length === 0}
      <div class="border border-dashed border-[var(--color-border)] py-8 px-4 text-center text-[var(--color-muted)] text-xs m-4">
        No messages yet. Agent communication will appear here.
      </div>
    {:else}
      {#each messages as msg (msg.id)}
        <div class="px-4 py-3 {messageContainerClass(msg)}">
          <!-- Header row: sender + type indicator + timestamp -->
          <div class="flex items-center gap-2 mb-1 flex-wrap">
            <span class="text-xs font-bold tracking-wider uppercase {senderColor(msg.sender)}">
              {senderLabel(msg.sender)}
            </span>
            {#if messageTypeLabel(msg.message_type)}
              <span class={messageTypeChipClass(msg.message_type)}>
                {messageTypeLabel(msg.message_type)}
              </span>
            {/if}
            <span class="text-[10px] text-[var(--color-muted)] ml-auto shrink-0">
              {formatTime(msg.created_at)}
            </span>
          </div>

          <!-- Message content -->
          <div class="text-xs text-[var(--color-text)] whitespace-pre-wrap leading-relaxed mt-1.5">
            {msg.content}
          </div>

          <!-- Action request options -->
          {#if msg.message_type === 'action_request'}
            {@const options = parseOptions(msg)}
            {#if options.length > 0}
              <div class="flex flex-wrap gap-2 mt-2">
                {#each options as option}
                  <button
                    onclick={() => onSend(option)}
                    {disabled}
                    class="px-3 py-1 text-xs font-bold tracking-wider border border-[var(--color-warning)]
                           text-[var(--color-warning)] hover:bg-[var(--color-warning)] hover:text-[var(--color-bg)]
                           transition-colors disabled:opacity-40 disabled:cursor-not-allowed"
                  >
                    {option}
                  </button>
                {/each}
              </div>
            {/if}
          {/if}
        </div>
      {/each}
    {/if}
  </div>

  <!-- Input area -->
  <div class="border-t border-[var(--color-border)] p-3 shrink-0">
    <div class="flex gap-2 items-end">
      <textarea
        bind:this={textareaEl}
        bind:value={inputValue}
        rows={1}
        placeholder="Type a message..."
        {disabled}
        onkeydown={handleKeydown}
        oninput={handleInput}
        class="flex-1 bg-[var(--color-surface)] border border-[var(--color-border)] px-3 py-2 text-xs
               text-[var(--color-text)] placeholder:text-[var(--color-muted)] focus:border-[var(--color-accent)]
               focus:outline-none resize-none overflow-y-auto disabled:opacity-40 disabled:cursor-not-allowed
               leading-relaxed"
        style="min-height: 2.5rem;"
      ></textarea>
      <button
        onclick={handleSend}
        disabled={disabled || !inputValue.trim()}
        class="px-4 h-9 bg-[var(--color-accent)] text-[var(--color-bg)] text-[10px] font-bold
               tracking-widest hover:opacity-90 transition-opacity disabled:opacity-40
               disabled:cursor-not-allowed shrink-0"
      >
        SEND
      </button>
    </div>
    <div class="text-[10px] text-[var(--color-muted)] mt-1">
      Enter to send · Shift+Enter for new line
    </div>
  </div>
</div>
