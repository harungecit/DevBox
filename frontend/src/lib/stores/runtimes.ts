import { writable, derived } from 'svelte/store';
import { EventsOn } from '../../../wailsjs/runtime/runtime';
import { GetRuntimeCatalog } from '../../../wailsjs/go/main/App';

// RuntimeMeta mirrors main.RuntimeMeta on the Go side: one entry per
// registered runtime — the five built-ins plus every installed vfox plugin.
// This catalog is the single source of runtime identity in the UI; no page
// hard-codes runtime names.
export interface RuntimeMeta {
  name: string;
  displayName: string;
  builtIn: boolean;
  plugin: boolean;
  kind: 'language' | 'tool' | string;
  description: string;
  homepage: string;
  license: string;
  pluginVersion: string;
  pluginUpdate: string;
  thirdParty: boolean;
  notes: string[];
  installed: number;
  global: string;
  envVars: Record<string, string>;
}

const _catalog = writable<RuntimeMeta[]>([]);
export const runtimeCatalog = { subscribe: _catalog.subscribe };

let loaded = false;
let listenersRegistered = false;

export async function loadRuntimeCatalog(): Promise<RuntimeMeta[]> {
  try {
    const list = ((await GetRuntimeCatalog()) || []) as RuntimeMeta[];
    _catalog.set(list);
    loaded = true;
    return list;
  } catch (e) {
    console.error('runtime catalog:', e);
    return [];
  }
}

// initRuntimeCatalog loads the catalog once and keeps it fresh on the events
// that change it (plugin added/removed, version installed). Idempotent.
export function initRuntimeCatalog() {
  if (listenersRegistered) return;
  listenersRegistered = true;
  EventsOn('runtimes:changed', () => { loadRuntimeCatalog(); });
  EventsOn('runtime:installed', () => { loadRuntimeCatalog(); });
  if (!loaded) loadRuntimeCatalog();
}

// runtimeLabel(name) → display name (falls back to the raw id).
export const runtimeLabel = derived(_catalog, (list) => (name: string): string =>
  list.find((m) => m.name === name)?.displayName ?? name
);

// runtimeMetaOf(name) → the entry, or undefined for unknown ids.
export const runtimeMetaOf = derived(_catalog, (list) => (name: string): RuntimeMeta | undefined =>
  list.find((m) => m.name === name)
);
