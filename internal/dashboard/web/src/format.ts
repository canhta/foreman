export function formatSender(jid: string): string {
  if (!jid) return '';
  return jid.replace(/@s\.whatsapp\.net$/, '');
}

export function formatTime(ts: string | null): string {
  if (!ts) return '';
  return new Date(ts).toLocaleTimeString();
}

export function formatRelative(ts: string | null): string {
  if (!ts) return '';
  const diff = Date.now() - new Date(ts).getTime();
  const mins = Math.floor(diff / 60000);
  if (mins < 1) return 'just now';
  if (mins < 60) return `${mins}m ago`;
  const hours = Math.floor(mins / 60);
  if (hours < 24) return `${hours}h ago`;
  return `${Math.floor(hours / 24)}d ago`;
}

export function formatCost(usd: number): string {
  return `$${usd.toFixed(2)}`;
}

export function formatTokens(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${Math.round(n / 1_000)}k`;
  return `${n}`;
}

export function severityIcon(severity: string): string {
  switch (severity) {
    case 'success': return '\u2713';
    case 'error': return '\u2717';
    case 'warning': return '\u26A0';
    default: return '\u25CF';
  }
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
