let token = localStorage.getItem('foreman_token') || '';
const headers = () => ({ Authorization: `Bearer ${token}` });

export function setToken(t: string) {
  token = t;
  localStorage.setItem('foreman_token', t);
}

export function getToken(): string {
  return token;
}

export async function fetchJSON<T>(url: string): Promise<T> {
  const res = await fetch(url, { headers: headers() });
  if (!res.ok) throw new Error(res.statusText);
  return res.json();
}

export async function postJSON<T>(url: string): Promise<T> {
  const res = await fetch(url, { method: 'POST', headers: headers() });
  if (!res.ok) throw new Error(res.statusText);
  return res.json();
}

export async function deleteJSON(url: string): Promise<void> {
  const res = await fetch(url, { method: 'DELETE', headers: headers() });
  if (!res.ok) throw new Error(res.statusText);
}
