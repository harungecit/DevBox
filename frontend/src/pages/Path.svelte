<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { t } from '../lib/i18n/index';
  import { serviceLogos, runtimeLogo } from '../lib/logos';
  import AppIcon from '../lib/components/AppIcon.svelte';
  import { appConfig } from '../lib/stores/app';
  import { runtimeCatalog } from '../lib/stores/runtimes';
  import type { RuntimeMeta } from '../lib/stores/runtimes';
  import { GetPATHEntries, AddToPATH, RemoveFromPATH, GetManagedEnv, GetPathHealth, CleanUserPath, CleanSystemPath, RemoveSystemPathEntry, GetPathEditor, SaveUserPath, SaveSystemPath, RefreshPath } from '../../wailsjs/go/main/App';

  // --- PATH editor: reorder / delete / add entries in either scope and save ---
  interface PathEntry { path: string; expanded: string; exists: boolean; managed: boolean }
  interface PathEditor { supported: boolean; system: PathEntry[]; user: PathEntry[] }
  let editor: PathEditor | null = null;
  let editScope: 'user' | 'system' = 'user';
  let draft: PathEntry[] = [];
  let original: string = '';
  let newEntry: string = '';
  let saving: boolean = false;
  let refreshing: boolean = false;
  let editorMessage: string = '';
  $: dirty = draft.map(d => d.path).join(';') !== original;

  async function loadEditor(keepScope: boolean = true) {
    try {
      editor = (await GetPathEditor()) as PathEditor;
      if (!editor.supported && editScope === 'system') editScope = 'user';
      setDraft();
    } catch (e) {
      console.error('path editor:', e);
    }
  }
  function setDraft() {
    if (!editor) return;
    const list = editScope === 'system' ? editor.system : editor.user;
    draft = list.map(e => ({ ...e }));
    original = draft.map(d => d.path).join(';');
    editorMessage = '';
  }
  function selectScope(scope: 'user' | 'system') {
    if (dirty && !confirm($t('path.editorDiscard'))) return;
    editScope = scope;
    setDraft();
  }
  function move(i: number, delta: number) {
    const j = i + delta;
    if (j < 0 || j >= draft.length) return;
    const next = [...draft];
    [next[i], next[j]] = [next[j], next[i]];
    draft = next;
  }
  function moveTop(i: number) {
    if (i === 0) return;
    const next = [...draft];
    const [item] = next.splice(i, 1);
    draft = [item, ...next];
  }
  function removeAt(i: number) {
    draft = draft.filter((_, idx) => idx !== i);
  }
  function addDraftEntry() {
    const p = newEntry.trim();
    if (!p) return;
    if (draft.some(d => d.path.toLowerCase().replace(/[\\/]+$/, '') === p.toLowerCase().replace(/[\\/]+$/, ''))) {
      editorMessage = $t('path.editorDuplicate');
      return;
    }
    draft = [...draft, { path: p, expanded: p, exists: true, managed: false }];
    newEntry = '';
  }
  async function saveDraft() {
    saving = true;
    errorMessage = '';
    editorMessage = '';
    try {
      const entries = draft.map(d => d.path);
      if (editScope === 'system') await SaveSystemPath(entries); else await SaveUserPath(entries);
      await RefreshPath();
      editorMessage = $t('path.editorSaved');
      await Promise.all([loadPATH(), loadHealth(), loadEditor()]);
    } catch (e) {
      errorMessage = String(e);
    }
    saving = false;
  }
  async function refreshPath() {
    refreshing = true;
    errorMessage = '';
    try {
      await RefreshPath();
      editorMessage = $t('path.editorRefreshed');
      await Promise.all([loadPATH(), loadHealth(), loadEditor()]);
    } catch (e) {
      errorMessage = String(e);
    }
    refreshing = false;
  }

  // PATH health: cmd.exe stops resolving anything once PATH passes 8191
  // characters (Laragon/XAMPP leftovers, duplicated copies, a literal %PATH%).
  interface PathHealth {
    supported: boolean; systemEntries: number; systemUnique: number; userEntries: number; userUnique: number;
    systemLength: number; userLength: number; combinedLength: number; limit: number;
    tooLong: boolean; literalPath: boolean;
    systemDuplicates: string[]; systemMissing: string[]; userDuplicates: string[]; userMissing: string[];
    systemAfter: number; userAfter: number; afterLength: number; issues: number;
    shadowed: { tool: string; expected: string; actual: string; system: boolean }[];
  }
  let unshadowing: string = '';
  async function unshadow(dir: string, system: boolean) {
    unshadowing = dir;
    errorMessage = '';
    try {
      if (system) await RemoveSystemPathEntry(dir); else await RemoveFromPATH(dir);
      cleanMessage = $t('path.shadowRemoved', dir);
      await Promise.all([loadPATH(), loadHealth()]);
    } catch (e) {
      errorMessage = String(e);
    }
    unshadowing = '';
  }
  let health: PathHealth | null = null;
  let cleaning: string = '';
  let cleanMessage: string = '';
  async function loadHealth() {
    try {
      const h = (await GetPathHealth()) as PathHealth;
      // Defensive: Go nil slices arrive as null; a null.length would abort Svelte's update loop.
      for (const k of ['systemDuplicates', 'systemMissing', 'userDuplicates', 'userMissing', 'shadowed'] as const) {
        if (!Array.isArray((h as any)[k])) (h as any)[k] = [];
      }
      health = h;
    } catch (e) {
      console.error('path health:', e);
    }
  }
  async function cleanPath(scope: 'user' | 'system') {
    cleaning = scope;
    cleanMessage = '';
    errorMessage = '';
    try {
      const removed = scope === 'user' ? await CleanUserPath() : await CleanSystemPath();
      cleanMessage = $t('path.healthCleaned', String(removed));
      await Promise.all([loadPATH(), loadHealth()]);
    } catch (e) {
      errorMessage = String(e);
    }
    cleaning = '';
  }
  import { EventsOn } from '../../wailsjs/runtime/runtime';

  // Environment variables DevBox wrote for plugin runtimes (JAVA_HOME…).
  interface ManagedEnvVar { key: string; value: string; runtime: string; version: string }
  let managedEnv: ManagedEnvVar[] = [];
  async function loadManagedEnv() {
    try {
      managedEnv = ((await GetManagedEnv()) || []) as ManagedEnvVar[];
    } catch (e) {
      console.error('managed env:', e);
    }
  }

  let pathEntries: string[] = [];
  let newPath: string = '';
  let loading: boolean = true;
  let errorMessage: string = '';

  // An entry is "managed" when it lives inside the DevBox data dir (current
  // location, or the legacy ~/.devbox one).
  function isManaged(entry: string): boolean {
    const norm = entry.replace(/\//g, '\\').toLowerCase();
    const dataDir = ($appConfig.dataDir || '').replace(/\//g, '\\').toLowerCase();
    if (dataDir && norm.startsWith(dataDir)) return true;
    return /[\\\/]\.devbox[\\\/]/i.test(entry);
  }

  $: managedEntries = pathEntries.filter(isManaged);
  $: systemEntries = pathEntries.filter(e => !isManaged(e));

  async function loadPATH() {
    loading = true;
    try {
      const entries = await GetPATHEntries();
      pathEntries = entries || [];
    } catch (e) {
      console.error('Failed to load PATH:', e);
    }
    loading = false;
  }

  async function addEntry() {
    if (!newPath.trim()) return;
    errorMessage = '';
    try {
      await AddToPATH(newPath.trim());
      newPath = '';
      await loadPATH();
    } catch (e) {
      errorMessage = String(e);
    }
  }

  async function removeEntry(path: string) {
    errorMessage = '';
    try {
      await RemoveFromPATH(path);
      await loadPATH();
    } catch (e) {
      errorMessage = String(e);
    }
  }

  // Identify what an entry belongs to from its path segments, e.g.
  //   <data>\runtimes\node\24.1.0  → node
  //   <data>\services\postgres\bin → postgres
  //   <data>\tools\bun             → bun
  interface EntryKind { label: string; icon: string; version: string }

  // Runtime ids → catalog entries (built-ins and plugins alike).
  $: catalogMap = Object.fromEntries($runtimeCatalog.map((m) => [m.name, m])) as Record<string, RuntimeMeta>;
  const serviceLabels: Record<string, string> = {
    nginx: 'Nginx', apache: 'Apache', caddy: 'Caddy', frankenphp: 'FrankenPHP', postgres: 'PostgreSQL',
    mysql: 'MySQL', mariadb: 'MariaDB', mongodb: 'MongoDB', redis: 'Redis', valkey: 'Valkey', mailpit: 'Mailpit',
  };
  const toolLabels: Record<string, string> = { bun: 'Bun', mkcert: 'mkcert', cloudflared: 'cloudflared', composer: 'Composer' };

  function classify(entry: string, catalog: Record<string, RuntimeMeta>): EntryKind {
    const parts = entry.split(/[\\/]/).filter(Boolean).map(p => p.toLowerCase());
    const at = (name: string) => parts.indexOf(name);

    const r = at('runtimes');
    if (r >= 0 && parts[r + 1]) {
      const id = parts[r + 1];
      const meta = catalog[id];
      return { label: meta?.displayName || id, icon: runtimeLogo(id, meta?.displayName), version: parts[r + 2] || '' };
    }
    const s = at('services');
    if (s >= 0 && parts[s + 1] && serviceLabels[parts[s + 1]]) {
      const id = parts[s + 1];
      return { label: serviceLabels[id], icon: serviceLogos[id] || '', version: '' };
    }
    const tl = at('tools');
    if (tl >= 0 && parts[tl + 1]) {
      const id = parts[tl + 1];
      return { label: toolLabels[id] || id, icon: '', version: '' };
    }
    return { label: 'DevBox', icon: '', version: '' };
  }

  let unsubs: Array<() => void> = [];
  onMount(() => {
    loadPATH();
    loadManagedEnv();
    loadHealth();
    loadEditor();
    unsubs.push(EventsOn('runtimes:changed', () => { loadPATH(); loadManagedEnv(); }));
    unsubs.push(EventsOn('runtime:installed', () => { loadPATH(); loadManagedEnv(); }));
  });
  onDestroy(() => { unsubs.forEach(fn => fn()); unsubs = []; });
</script>

<div class="space-y-6">
  <div>
    <h2 class="text-2xl font-bold">{$t('path.title')}</h2>
    <p class="text-[var(--color-text-secondary)] mt-1">{$t('path.subtitle')}</p>
  </div>

  <!-- Info Box -->
  <div class="card bg-blue-500/5 border-blue-500/20">
    <div class="flex gap-3">
      <svg class="w-5 h-5 text-blue-500 flex-shrink-0 mt-0.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
        <circle cx="12" cy="12" r="10"/><line x1="12" y1="16" x2="12" y2="12"/><line x1="12" y1="8" x2="12.01" y2="8"/>
      </svg>
      <p class="text-sm text-[var(--color-text-secondary)] leading-relaxed">{$t('path.description')}</p>
    </div>
  </div>

  <!-- PATH health -->
  {#if health && health.supported && (health.issues > 0 || cleanMessage)}
    <div class="card {health.tooLong ? 'bg-red-500/5 border-red-500/30' : 'bg-amber-500/5 border-amber-500/20'} p-5">
      <div class="flex items-start justify-between gap-4">
        <div class="min-w-0">
          <h3 class="text-base font-bold flex items-center gap-2">
            <svg class="w-5 h-5 {health.tooLong ? 'text-red-500' : 'text-amber-500'}" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"/><line x1="12" y1="9" x2="12" y2="13"/><line x1="12" y1="17" x2="12.01" y2="17"/></svg>
            {health.tooLong ? $t('path.healthTooLongTitle') : $t('path.healthTitle')}
          </h3>
          <p class="text-xs text-[var(--color-text-secondary)] mt-1">
            {$t('path.healthStats', String(health.combinedLength), String(health.limit), String(health.systemEntries), String(health.systemUnique), String(health.userEntries))}
          </p>
          {#if health.tooLong}
            <p class="text-xs text-red-600 dark:text-red-400 mt-1">{$t('path.healthTooLongDesc')}</p>
          {/if}
          <ul class="text-xs text-[var(--color-text-secondary)] mt-2 space-y-0.5 list-disc pl-4">
            {#if health.literalPath}<li>{$t('path.healthLiteral')}</li>{/if}
            {#if health.systemDuplicates.length + health.userDuplicates.length > 0}<li>{$t('path.healthDuplicates', String(health.systemDuplicates.length + health.userDuplicates.length))}</li>{/if}
            {#if health.systemMissing.length + health.userMissing.length > 0}<li>{$t('path.healthMissing', String(health.systemMissing.length + health.userMissing.length))}</li>{/if}
          </ul>
          {#if health.tooLong || health.literalPath || health.systemDuplicates.length + health.userDuplicates.length + health.systemMissing.length + health.userMissing.length > 0}
            <p class="text-xs text-[var(--color-text-secondary)] mt-2">{$t('path.healthAfter', String(health.afterLength), String(health.systemAfter), String(health.userAfter))}</p>
          {/if}
          {#if health.shadowed && health.shadowed.length > 0}
            <div class="mt-3 pt-3 border-t border-[var(--color-border)]">
              <p class="text-xs font-bold mb-1">{$t('path.shadowTitle')}</p>
              <p class="text-[11px] text-[var(--color-text-secondary)] mb-2">{$t('path.shadowDesc')}</p>
              <div class="space-y-1.5">
                {#each health.shadowed as s (s.tool + s.actual)}
                  <div class="flex items-center justify-between gap-3 text-[11px] px-2.5 py-1.5 rounded-lg bg-[var(--color-bg)] border border-[var(--color-border)]">
                    <div class="min-w-0">
                      <span class="font-mono font-bold">{s.tool}</span>
                      <span class="text-[var(--color-text-secondary)]"> → </span>
                      <span class="font-mono truncate" title={s.actual}>{s.actual}</span>
                      <span class="text-[9px] px-1 ml-1 rounded bg-slate-500/10 text-slate-500 uppercase font-bold">{s.system ? $t('path.shadowSystem') : $t('path.shadowUser')}</span>
                      <p class="text-[10px] text-[var(--color-text-secondary)] truncate" title={s.expected}>{$t('path.shadowExpected', s.expected)}</p>
                    </div>
                    <button class="text-[10px] px-2 py-1 rounded-lg text-red-500 border border-red-500/20 hover:bg-red-500/10 shrink-0 disabled:opacity-50" on:click={() => unshadow(s.actual, s.system)} disabled={unshadowing !== ''} title={$t('path.shadowRemoveHint')}>
                      {unshadowing === s.actual ? $t('common.loading') : (s.system ? $t('path.shadowRemoveSystem') : $t('path.shadowRemoveUser'))}
                    </button>
                  </div>
                {/each}
              </div>
            </div>
          {/if}
          {#if cleanMessage}<p class="text-xs text-emerald-500 mt-2">{cleanMessage}</p>{/if}
        </div>
        {#if health.tooLong || health.literalPath || health.systemDuplicates.length + health.userDuplicates.length + health.systemMissing.length + health.userMissing.length > 0}
          <div class="flex flex-col gap-2 shrink-0">
            <button class="text-xs px-3 py-1.5 rounded-lg font-medium bg-primary-600 hover:bg-primary-700 text-white shadow-sm disabled:opacity-50" on:click={() => cleanPath('system')} disabled={cleaning !== ''} title={$t('path.healthCleanSystemHint')}>
              {cleaning === 'system' ? $t('common.loading') : $t('path.healthCleanSystem')}
            </button>
            <button class="text-xs px-3 py-1.5 rounded-lg font-medium border border-[var(--color-border)] hover:bg-[var(--color-bg)] disabled:opacity-50" on:click={() => cleanPath('user')} disabled={cleaning !== ''}>
              {cleaning === 'user' ? $t('common.loading') : $t('path.healthCleanUser')}
            </button>
          </div>
        {/if}
      </div>
    </div>
  {/if}

  <!-- Error message -->
  {#if errorMessage}
    <div class="p-3 rounded-lg bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 text-red-700 dark:text-red-400 text-sm">
      {errorMessage}
    </div>
  {/if}

  <!-- DevBox Managed Entries -->
  <div class="card p-5">
    <div class="mb-5">
      <h3 class="text-base font-bold flex items-center gap-2">
        <svg class="w-5 h-5 text-blue-500" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
          <path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/>
        </svg>
        {$t('path.managed')}
        <span class="badge badge-info">{managedEntries.length}</span>
      </h3>
      <p class="text-xs text-[var(--color-text-secondary)] mt-1">{$t('path.managedDesc')}</p>
    </div>

    {#if managedEntries.length > 0}
      <div class="space-y-2">
        {#each managedEntries as entry}
          {@const kind = classify(entry, catalogMap)}
          <div class="flex items-center justify-between p-3 rounded-xl border border-slate-100 dark:border-slate-800 hover:bg-slate-50 dark:hover:bg-slate-800/50 transition-all group">
            <div class="flex items-center gap-4 flex-1 min-w-0">
              <div class="w-8 h-8 rounded-lg overflow-hidden flex-shrink-0 shadow-sm bg-white dark:bg-slate-700 p-1 flex items-center justify-center">
                {#if kind.icon}
                  {@html kind.icon}
                {:else}
                  <AppIcon size="w-6 h-6" />
                {/if}
              </div>
              <div class="truncate">
                <p class="text-xs font-bold text-[var(--color-text)] flex items-center gap-1.5">
                  {kind.label}
                  {#if kind.version}<span class="font-mono font-medium text-[10px] text-primary-500">{kind.version}</span>{/if}
                </p>
                <p class="font-mono text-[10px] text-slate-500 truncate">{entry}</p>
              </div>
            </div>
            <button
              class="btn-icon text-red-500 opacity-0 group-hover:opacity-100 transition-all hover:bg-red-50 dark:hover:bg-red-900/20"
              on:click={() => removeEntry(entry)}
              title={$t('path.remove')}
            >
              <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M3 6h18M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/></svg>
            </button>
          </div>
        {/each}
      </div>
    {:else if !loading}
      <div class="text-center py-12 text-[var(--color-text-secondary)] bg-slate-50/50 dark:bg-slate-800/20 rounded-xl border-2 border-dashed border-slate-200 dark:border-slate-800">
        <svg class="w-12 h-12 mx-auto mb-3 opacity-20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
          <path d="M4 17l6-5-6-5m8 10h8" />
        </svg>
        <p class="text-sm font-medium">{$t('path.empty')}</p>
        <p class="text-xs mt-1 opacity-60">{$t('path.emptyHint')}</p>
      </div>
    {/if}
  </div>

  <!-- Environment variables managed by DevBox (plugin runtimes) -->
  <div class="card p-5">
    <div class="mb-4">
      <h3 class="text-base font-bold flex items-center gap-2">
        <svg class="w-5 h-5 text-purple-500" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
          <polyline points="4 17 10 11 4 5"/><line x1="12" y1="19" x2="20" y2="19"/>
        </svg>
        {$t('path.envTitle')}
        <span class="badge badge-info">{managedEnv.length}</span>
      </h3>
      <p class="text-xs text-[var(--color-text-secondary)] mt-1">{$t('path.envDesc')}</p>
    </div>
    {#if managedEnv.length > 0}
      <div class="space-y-1.5">
        {#each managedEnv as v (v.key)}
          {@const meta = catalogMap[v.runtime]}
          <div class="flex items-center gap-3 p-2.5 rounded-xl border border-slate-100 dark:border-slate-800">
            <div class="w-7 h-7 rounded-lg overflow-hidden flex-shrink-0 bg-white dark:bg-slate-700 p-1">{@html runtimeLogo(v.runtime, meta?.displayName)}</div>
            <div class="min-w-0 flex-1">
              <p class="text-xs font-bold font-mono">{v.key}</p>
              <p class="font-mono text-[10px] text-slate-500 truncate" title={v.value}>{v.value}</p>
            </div>
            <span class="text-[10px] text-[var(--color-text-secondary)] shrink-0">{meta?.displayName || v.runtime} <span class="font-mono text-primary-500">{v.version}</span></span>
          </div>
        {/each}
      </div>
    {:else}
      <p class="text-xs text-[var(--color-text-secondary)] italic">{$t('path.envEmpty')}</p>
    {/if}
  </div>

  <!-- Add Custom Entry -->
  <div class="card">
    <h3 class="text-sm font-semibold mb-3">{$t('path.addCustom')}</h3>
    <div class="flex gap-2">
      <input
        type="text"
        bind:value={newPath}
        placeholder={$t('path.addCustomPlaceholder')}
        class="flex-1 px-3 py-2 rounded-lg border border-[var(--color-border)] bg-[var(--color-bg)] text-sm font-mono focus:outline-none focus:ring-2 focus:ring-primary-500/50"
        on:keydown={(e) => { if (e.key === 'Enter') addEntry(); }}
      />
      <button class="btn-primary" on:click={addEntry}>{$t('path.add')}</button>
    </div>
  </div>

  <!-- PATH editor: user + system scopes, reorder / delete / add, save -->
  <div class="card p-5">
    <div class="flex items-start justify-between gap-4 mb-4">
      <div>
        <h3 class="text-base font-bold flex items-center gap-2">
          <svg class="w-5 h-5 text-primary-500" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7"/><path d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z"/></svg>
          {$t('path.editorTitle')}
        </h3>
        <p class="text-xs text-[var(--color-text-secondary)] mt-1">{$t('path.editorDesc')}</p>
      </div>
      <button class="text-xs px-3 py-1.5 rounded-lg font-medium border border-[var(--color-border)] hover:bg-[var(--color-bg)] disabled:opacity-50 shrink-0" on:click={refreshPath} disabled={refreshing} title={$t('path.editorRefreshHint')}>
        {refreshing ? $t('common.loading') : $t('path.editorRefresh')}
      </button>
    </div>

    <div class="flex items-center gap-1 border-b border-[var(--color-border)] mb-3">
      <button class="px-3 py-2 text-sm font-medium border-b-2 -mb-px {editScope === 'user' ? 'border-primary-500 text-primary-500' : 'border-transparent text-[var(--color-text-secondary)]'}" on:click={() => selectScope('user')}>
        {$t('path.editorUser')} <span class="text-[10px] text-[var(--color-text-secondary)]">{editor?.user.length ?? 0}</span>
      </button>
      {#if editor?.supported}
        <button class="px-3 py-2 text-sm font-medium border-b-2 -mb-px {editScope === 'system' ? 'border-primary-500 text-primary-500' : 'border-transparent text-[var(--color-text-secondary)]'}" on:click={() => selectScope('system')}>
          {$t('path.editorSystem')} <span class="text-[10px] text-[var(--color-text-secondary)]">{editor?.system.length ?? 0}</span>
        </button>
      {/if}
      {#if dirty}
        <span class="ml-auto text-[10px] font-bold uppercase px-1.5 py-0.5 rounded bg-amber-500/10 text-amber-600 dark:text-amber-400 border border-amber-500/20">{$t('path.editorUnsaved')}</span>
      {/if}
    </div>

    {#if editScope === 'system'}
      <p class="text-[11px] text-[var(--color-text-secondary)] mb-2">{$t('path.editorSystemHint')}</p>
    {/if}

    {#if draft.length > 0}
      <div class="space-y-1 max-h-[420px] overflow-y-auto pr-1">
        {#each draft as e, i (e.path + i)}
          <div class="flex items-center gap-2 px-2 py-1.5 rounded-lg border {e.exists ? 'border-[var(--color-border)]' : 'border-red-500/30 bg-red-500/5'} group">
            <span class="w-6 text-[10px] text-[var(--color-text-secondary)] font-mono text-right shrink-0">{i + 1}</span>
            <div class="min-w-0 flex-1">
              <p class="font-mono text-xs truncate" title={e.expanded}>{e.path}</p>
              {#if !e.exists}<p class="text-[10px] text-red-500">{$t('path.editorMissing')}</p>{/if}
            </div>
            {#if e.managed}<span class="text-[9px] px-1 rounded bg-primary-500/10 text-primary-500 uppercase font-bold shrink-0">DevBox</span>{/if}
            <div class="flex items-center gap-0.5 shrink-0 opacity-60 group-hover:opacity-100">
              <button class="btn-icon" on:click={() => moveTop(i)} disabled={i === 0} title={$t('path.editorMoveTop')}>
                <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><polyline points="17 11 12 6 7 11"/><polyline points="17 18 12 13 7 18"/></svg>
              </button>
              <button class="btn-icon" on:click={() => move(i, -1)} disabled={i === 0} title={$t('path.editorMoveUp')}>
                <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><polyline points="18 15 12 9 6 15"/></svg>
              </button>
              <button class="btn-icon" on:click={() => move(i, 1)} disabled={i === draft.length - 1} title={$t('path.editorMoveDown')}>
                <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><polyline points="6 9 12 15 18 9"/></svg>
              </button>
              <button class="btn-icon text-red-500 hover:bg-red-500/10" on:click={() => removeAt(i)} title={$t('path.remove')}>
                <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M3 6h18M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/></svg>
              </button>
            </div>
          </div>
        {/each}
      </div>
    {:else}
      <p class="text-sm text-[var(--color-text-secondary)] text-center py-4">{editor ? $t('path.empty') : $t('common.loading')}</p>
    {/if}

    <div class="flex gap-2 mt-3">
      <input
        type="text"
        bind:value={newEntry}
        placeholder={$t('path.addCustomPlaceholder')}
        class="flex-1 px-3 py-2 rounded-lg border border-[var(--color-border)] bg-[var(--color-bg)] text-sm font-mono focus:outline-none focus:ring-2 focus:ring-primary-500/50"
        on:keydown={(e) => { if (e.key === 'Enter') addDraftEntry(); }}
      />
      <button class="text-xs px-3 py-1.5 rounded-lg font-medium border border-[var(--color-border)] hover:bg-[var(--color-bg)]" on:click={addDraftEntry}>{$t('path.add')}</button>
    </div>

    <div class="flex items-center justify-between mt-4">
      <p class="text-xs {errorMessage ? 'text-red-500' : 'text-emerald-500'}">{editorMessage}</p>
      <div class="flex items-center gap-2">
        <button class="text-xs px-3 py-1.5 rounded-lg font-medium border border-[var(--color-border)] hover:bg-[var(--color-bg)] disabled:opacity-50" on:click={setDraft} disabled={!dirty || saving}>{$t('path.editorReset')}</button>
        <button class="btn-primary text-xs disabled:opacity-50" on:click={saveDraft} disabled={!dirty || saving} title={editScope === 'system' ? $t('path.editorSaveSystemHint') : ''}>
          {#if saving}<div class="w-3 h-3 border-2 border-white border-t-transparent rounded-full animate-spin inline-block mr-1"></div>{/if}
          {editScope === 'system' ? $t('path.editorSaveSystem') : $t('path.editorSaveUser')}
        </button>
      </div>
    </div>
  </div>
</div>
