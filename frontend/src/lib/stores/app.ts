import { writable } from 'svelte/store';
import { GetConfig } from '../../../wailsjs/go/main/App';

// Current active page
export const currentPage = writable<string>('dashboard');

// Theme store
export const theme = writable<string>('dark');

// Config store
export interface AppConfig {
  language: string;
  theme: string;
  autoStart: boolean;
  startMinimized: boolean;
  closeToTray: boolean;
  dataDir: string;
  activeRuntimes: Record<string, string>;
  autoStartSvcs: string[];
  proxyEnabled: boolean;
  versionCacheHours: number;
  terminal?: string;
}

export const appConfig = writable<AppConfig>({
  language: 'en',
  theme: 'dark',
  autoStart: false,
  startMinimized: false,
  closeToTray: true,
  dataDir: '',
  activeRuntimes: {},
  autoStartSvcs: [],
  proxyEnabled: false,
  versionCacheHours: 48,
});

// Load config from backend
export async function loadConfig() {
  try {
    const cfg = await GetConfig();
    appConfig.set(cfg as unknown as AppConfig);
    theme.set(cfg.theme || 'dark');
  } catch (e) {
    console.error('Failed to load config:', e);
  }
}

// Apply theme to document
export function applyTheme(t: string) {
  const root = document.documentElement;

  // Clear any existing theme classes
  root.classList.remove('dark', 'light');

  if (t === 'system') {
    const isDark = window.matchMedia('(prefers-color-scheme: dark)').matches;
    if (isDark) {
      root.classList.add('dark');
      root.style.colorScheme = 'dark';
    } else {
      root.classList.add('light');
      root.style.colorScheme = 'light';
    }
  } else if (t === 'dark') {
    root.classList.add('dark');
    root.style.colorScheme = 'dark';
  } else {
    root.classList.add('light');
    root.style.colorScheme = 'light';
  }

  // Save to localStorage for quick boot persistence
  localStorage.setItem('devbox-theme', t);
}

// Global listener for system theme changes
if (typeof window !== 'undefined') {
  window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', (e) => {
    const savedTheme = localStorage.getItem('devbox-theme') || 'system';
    if (savedTheme === 'system') {
      applyTheme('system');
    }
  });
}
