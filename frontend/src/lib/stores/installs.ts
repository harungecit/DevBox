import { writable } from 'svelte/store';
import { EventsOn } from '../../../wailsjs/runtime/runtime';

export interface RuntimeInstallState {
  version: string;
  percent: number;
  message: string;
  error?: string;
  // true when this is an in-place import (link), not a download — the UI
  // shows "Importing..." instead of "Downloading...".
  importing?: boolean;
}

export interface ServiceInstallState {
  percent: number;
  message: string;
  error?: string;
  importing?: boolean;
}

// Map of runtime-name → in-flight install state. Present while an install is
// active or just errored; cleared on success. Survives page unmount so users
// can leave the Runtimes page and return without losing progress visibility.
const _runtimeInstalls = writable<Record<string, RuntimeInstallState>>({});
export const runtimeInstalls = { subscribe: _runtimeInstalls.subscribe };

// Same idea for services. Keyed by service name (e.g. "redis", "nginx").
const _serviceInstalls = writable<Record<string, ServiceInstallState>>({});
export const serviceInstalls = { subscribe: _serviceInstalls.subscribe };

// vfox plugin jobs (install / update). Keyed by plugin name — or by the
// source URL for "install from URL", until the plugin's real name is known.
export interface PluginJobState {
  action: string;
  message: string;
  error?: string;
}
const _pluginInstalls = writable<Record<string, PluginJobState>>({});
export const pluginInstalls = { subscribe: _pluginInstalls.subscribe };

let listenersRegistered = false;

// initInstallListeners attaches global Wails listeners that keep both stores
// in sync with backend runtime:* and service:* events. Idempotent — safe to
// import from multiple modules; listeners only attach the first time.
export function initInstallListeners() {
  if (listenersRegistered) return;
  listenersRegistered = true;

  // --- Runtime install events ---

  EventsOn('runtime:progress', (data: any) => {
    if (!data?.name) return;
    _runtimeInstalls.update(s => ({
      ...s,
      [data.name]: {
        version: data.version ?? s[data.name]?.version ?? '',
        percent: typeof data.percent === 'number' ? data.percent : 0,
        message: data.message ?? '',
        error: undefined,
        importing: s[data.name]?.importing,
      },
    }));
  });

  EventsOn('runtime:installed', (data: any) => {
    if (!data?.name) return;
    _runtimeInstalls.update(s => {
      const { [data.name]: _removed, ...rest } = s;
      return rest;
    });
  });

  EventsOn('runtime:error', (data: any) => {
    if (!data?.name) return;
    _runtimeInstalls.update(s => ({
      ...s,
      [data.name]: {
        ...(s[data.name] ?? { version: '', percent: 0, message: '' }),
        error: data.error ?? 'Unknown error',
      },
    }));
  });

  // --- Plugin (vfox) job events ---

  EventsOn('plugin:progress', (data: any) => {
    if (!data?.name) return;
    _pluginInstalls.update(s => ({
      ...s,
      [data.name]: { action: data.action ?? s[data.name]?.action ?? 'install', message: data.message ?? '', error: undefined },
    }));
  });

  EventsOn('plugin:installed', (data: any) => {
    if (!data?.name) return;
    _pluginInstalls.update(s => {
      const { [data.name]: _removed, ...rest } = s;
      return rest;
    });
  });

  EventsOn('plugin:error', (data: any) => {
    if (!data?.name) return;
    _pluginInstalls.update(s => ({
      ...s,
      [data.name]: {
        ...(s[data.name] ?? { action: 'install', message: '' }),
        error: data.error ?? 'Unknown error',
      },
    }));
  });

  // --- Service install events ---

  EventsOn('service:progress', (data: any) => {
    if (!data?.name) return;
    _serviceInstalls.update(s => ({
      ...s,
      [data.name]: {
        percent: typeof data.percent === 'number' ? data.percent : 0,
        message: data.message ?? '',
        error: undefined,
        importing: s[data.name]?.importing,
      },
    }));
  });

  EventsOn('service:installed', (data: any) => {
    if (!data?.name) return;
    _serviceInstalls.update(s => {
      const { [data.name]: _removed, ...rest } = s;
      return rest;
    });
  });

  EventsOn('service:error', (data: any) => {
    if (!data?.name) return;
    _serviceInstalls.update(s => ({
      ...s,
      [data.name]: {
        ...(s[data.name] ?? { percent: 0, message: '' }),
        error: data.error ?? 'Unknown error',
      },
    }));
  });
}

// Back-compat alias for the previous name; both wire up runtime and service
// listeners now since they share the same idempotent registration.
export const initRuntimeInstallListeners = initInstallListeners;

// Call when the user clicks Install on a runtime — the UI flips to "starting…"
// immediately, without waiting for the first backend progress event.
export function startRuntimeInstall(name: string, version: string, importing: boolean = false) {
  _runtimeInstalls.update(s => ({
    ...s,
    [name]: { version, percent: 0, message: '', error: undefined, importing },
  }));
}

export function clearRuntimeInstall(name: string) {
  _runtimeInstalls.update(s => {
    const { [name]: _removed, ...rest } = s;
    return rest;
  });
}

// Service equivalents.
export function startServiceInstall(name: string, initialMessage: string = '', importing: boolean = false) {
  _serviceInstalls.update(s => ({
    ...s,
    [name]: { percent: 0, message: initialMessage, error: undefined, importing },
  }));
}

export function clearServiceInstall(name: string) {
  _serviceInstalls.update(s => {
    const { [name]: _removed, ...rest } = s;
    return rest;
  });
}

// Plugin equivalents.
export function startPluginJob(name: string, action: string = 'install') {
  _pluginInstalls.update(s => ({ ...s, [name]: { action, message: '', error: undefined } }));
}

export function clearPluginJob(name: string) {
  _pluginInstalls.update(s => {
    const { [name]: _removed, ...rest } = s;
    return rest;
  });
}
