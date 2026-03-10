export function formatSender(id: string): string {
  if (!id) return '';
  return id;
}

export function formatTime(ts: string | null): string {
  if (!ts) return '';
  return new Date(ts).toLocaleTimeString();
}

export function formatRelative(ts: string | null): string {
  if (!ts) return '';
  const date = new Date(ts);
  // Guard against zero/invalid timestamps (before year 2000)
  if (isNaN(date.getTime()) || date.getFullYear() < 2000) return '';
  const diff = Date.now() - date.getTime();
  if (diff < 0) return 'just now';
  const mins = Math.floor(diff / 60000);
  if (mins < 1) return 'just now';
  if (mins < 60) return `${mins}m ago`;
  const hours = Math.floor(mins / 60);
  if (hours < 24) return `${hours}h ago`;
  const days = Math.floor(hours / 24);
  if (days < 30) return `${days}d ago`;
  const months = Math.floor(days / 30);
  if (months < 12) return `${months}mo ago`;
  return `${Math.floor(months / 12)}y ago`;
}

export function formatCost(usd: number | null | undefined): string {
  if (usd == null) return '$0.00';
  return `$${usd.toFixed(2)}`;
}

export function formatTokens(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${Math.round(n / 1_000)}k`;
  return `${n}`;
}

/** Returns today's date as YYYY-MM-DD in local timezone */
export function localDateStr(d: Date = new Date()): string {
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
}

export function runnerBadgeCls(runner: string): string {
  if (runner === 'claudecode') return 'text-accent border-accent/40';
  if (runner === 'copilot') return 'text-purple-400 border-purple-400/40';
  return 'text-muted border-border-strong';
}

export function shortModel(model: string): string {
  return model.replace(/^(claude-|gpt-)/, '');
}

/** Split text into alternating text/url parts for linkified rendering. */
export function linkifyParts(text: string): { type: 'text' | 'url'; content: string }[] {
  if (!text) return [];
  const urlRegex = /(https?:\/\/[^\s]+)/g;
  const parts: { type: 'text' | 'url'; content: string }[] = [];
  let lastIndex = 0;
  let match: RegExpExecArray | null;
  while ((match = urlRegex.exec(text)) !== null) {
    if (match.index > lastIndex) parts.push({ type: 'text', content: text.slice(lastIndex, match.index) });
    parts.push({ type: 'url', content: match[0] });
    lastIndex = match.index + match[0].length;
  }
  if (lastIndex < text.length) parts.push({ type: 'text', content: text.slice(lastIndex) });
  return parts;
}

export function taskIcon(status: string): string {
  switch (status) {
    case 'done': return '\u2713';
    case 'failed': return '\u2717';
    case 'implementing': case 'tdd_verifying': case 'testing':
    case 'spec_review': case 'quality_review': return '\u2699';
    case 'skipped': return '\u2298';
    default: return '\u25CB';
  }
}
