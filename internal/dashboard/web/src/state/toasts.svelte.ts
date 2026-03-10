export interface Toast {
  id: string;
  message: string;
  ticketId?: string;
  severity: string;
  createdAt: number;
}

class ToastState {
  toasts = $state<Toast[]>([]);

  add(message: string, severity = 'info', ticketId?: string) {
    const toast: Toast = {
      id: crypto.randomUUID(),
      message,
      ticketId,
      severity,
      createdAt: Date.now(),
    };
    this.toasts = [toast, ...this.toasts].slice(0, 10);
    setTimeout(() => this.remove(toast.id), 8000);
  }

  remove(id: string) {
    this.toasts = this.toasts.filter(t => t.id !== id);
  }
}

export const toasts = new ToastState();
