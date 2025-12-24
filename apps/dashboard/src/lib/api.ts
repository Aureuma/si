export type DyadTask = {
  id: number;
  title: string;
  kind: string;
  status: string;
  priority: string;
  dyad: string;
  actor?: string;
  critic?: string;
  claimed_by?: string;
  updated_at?: string;
};

const apiBase = '/api';

export async function fetchTasks(): Promise<DyadTask[]> {
  const res = await fetch(`${apiBase}/dyad-tasks`);
  if (!res.ok) throw new Error(`Failed to load tasks: ${res.status}`);
  return res.json();
}

export async function spawnDyad(name: string, role: string, dept: string): Promise<string> {
  const res = await fetch(`${apiBase}/spawn`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name, role, department: dept })
  });
  if (!res.ok) throw new Error(`Spawn failed: ${res.status}`);
  const data = await res.json();
  return data.output || 'ok';
}

