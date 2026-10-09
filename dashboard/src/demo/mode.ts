/**
 * Demo mode: the full dashboard with sample data and no server, for visitors
 * who want to look around without an account ("Explore the demo" on the
 * landing page). The flag lives in sessionStorage, so it only affects this
 * browser tab and ends when the tab is closed.
 */
const KEY = 'agentmesh-demo';
let memory = false; // fallback when sessionStorage is unavailable (private mode, blocked storage)

export function isDemo(): boolean {
  try {
    return sessionStorage.getItem(KEY) === '1' || memory;
  } catch {
    return memory;
  }
}

export function enterDemo(): void {
  memory = true;
  try {
    sessionStorage.setItem(KEY, '1');
  } catch {
    // memory flag is enough for this page load
  }
}

export function exitDemo(): void {
  memory = false;
  try {
    sessionStorage.removeItem(KEY);
  } catch {
    // nothing stored
  }
}
