let token = localStorage.getItem('foreman_token') || '';
const headers = () => ({ Authorization: `Bearer ${token}` });

let onUnauthorized: (() => void) | null = null;
let unauthorizedFired = false;

export function setOnUnauthorized(cb: () => void) {
  onUnauthorized = cb;
}

export function setToken(t: string) {
  token = t;
  unauthorizedFired = false;
  localStorage.setItem('foreman_token', t);
}

export function getToken(): string {
  return token;
}

export function clearToken() {
  token = '';
  localStorage.removeItem('foreman_token');
}

function handleResponse(res: Response): Response {
  if (res.status === 401 || res.status === 403) {
    if (!unauthorizedFired) {
      unauthorizedFired = true;
      clearToken();
      onUnauthorized?.();
    }
    throw new Error('Unauthorized');
  }
  if (!res.ok) throw new Error(res.statusText);
  return res;
}

export async function fetchJSON<T>(url: string): Promise<T> {
  if (!token) throw new Error('No token');
  const res = await fetch(url, { headers: headers() });
  return handleResponse(res).json();
}

export async function postJSON<T>(url: string): Promise<T> {
  if (!token) throw new Error('No token');
  const res = await fetch(url, { method: 'POST', headers: headers() });
  return handleResponse(res).json();
}

export async function deleteJSON(url: string): Promise<void> {
  if (!token) throw new Error('No token');
  const res = await fetch(url, { method: 'DELETE', headers: headers() });
  handleResponse(res);
}
