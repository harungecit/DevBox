import { writable } from 'svelte/store';
import { EventsOn } from '../../../wailsjs/runtime/runtime';

export interface RuntimeInstallState {
  version: string;
  percent: number;
  message: string;
  error?: string;
}

export interface ServiceInstallState {
  percent: number;
  message: string;
  error?: string;
}

// Map of runtime-name → in-flight install state. Present while an install is
// active or just errored; cleared on success. Survives page unmount so users
// can leave the Runtimes page and return without losing progress visibility.
const _runtimeInstalls = writable<Record<string, RuntimeInstallState>>({});
export const runtimeInstalls = { subscribe: _runtimeInstalls.subscribe };

// Same idea for services. Keyed by service name (e.g. "redis", "nginx").
const _serviceInstalls = writable<Record<string, ServiceInstallState>>({});
export const serviceInstalls = { subscribe: _serviceInstalls.subscribe };

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

  // --- Service install events ---

  EventsOn('service:progress', (data: any) => {
    if (!data?.name) return;
    _serviceInstalls.update(s => ({
      ...s,
      [data.name]: {
        percent: typeof data.percent === 'number' ? data.percent : 0,
        message: data.message ?? '',
        error: undefined,
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
export function startRuntimeInstall(name: string, version: string) {
  _runtimeInstalls.update(s => ({
    ...s,
    [name]: { version, percent: 0, message: '', error: undefined },
  }));
}

export function clearRuntimeInstall(name: string) {
  _runtimeInstalls.update(s => {
    const { [name]: _removed, ...rest } = s;
    return rest;
  });
}

// Service equivalents.
export function startServiceInstall(name: string, initialMessage: string = '') {
  _serviceInstalls.update(s => ({
    ...s,
    [name]: { percent: 0, message: initialMessage, error: undefined },
  }));
}

export function clearServiceInstall(name: string) {
  _serviceInstalls.update(s => {
    const { [name]: _removed, ...rest } = s;
    return rest;
  });
}
