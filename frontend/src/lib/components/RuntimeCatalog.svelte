<script lang="ts">
  import { createEventDispatcher, onMount, onDestroy } from 'svelte';
  import { t } from '../i18n/index';
  import { runtimeLogo } from '../logos';
  import { GetVfoxRegistry, GetVfoxPluginManifest, InstallVfoxPlugin, InstallVfoxPluginFromURL, UpdateVfoxPlugin, RemoveVfoxPlugin, CheckVfoxPluginUpdates } from '../../../wailsjs/go/main/App';
  import { BrowserOpenURL, EventsOn } from '../../../wailsjs/runtime/runtime';
  import { pluginInstalls, startPluginJob, clearPluginJob } from '../stores/installs';
  import ConfirmDialog from './ConfirmDialog.svelte';

  // The "Add runtime" catalog: every plugin the vfox registry offers, merged
  // with what is installed locally. Opened from the Runtimes page.
  export let open: boolean = false;
  const dispatch = createEventDispatcher();

  interface Entry {
    name: string;
    displayName: string;
    desc: string;
    homepage: string;
    kind: string;
    installed: boolean;
    installedVersion: string;
    updateAvailable: string;
    builtIn: boolean;
    builtInAs: string;
    thirdParty: boolean;
  }

  interface Manifest {
    name: string;
    version: string;
    description: string;
    homepage: string;
    license: string;
    minRuntimeVersion: string;
    notes: string[];
    legacyFilenames: string[];
  }

  let entries: Entry[] = [];
  let loading: boolean = false;
  let loaded: boolean = false;
  let error: string = '';
  let search: string = '';
  let registryUrl: string = '';
  let fetchedAt: string = '';
  let showUrlInput: boolean = false;
  let sourceUrl: string = '';
  let details: Record<string, Manifest | null> = {};
  let expanded: string = '';
  let checkingUpdates: boolean = false;

  $: if (open && !loaded && !loading) load(false);

  $: filtered = entries.filter((e) => {
    if (!search.trim()) return true;
    const q = search.toLowerCase();
    return e.name.toLowerCase().includes(q) || e.displayName.toLowerCase().includes(q) || (e.desc || '').toLowerCase().includes(q);
  });
  $: installedEntries = filtered.filter((e) => e.installed);
  $: availableEntries = filtered.filter((e) => !e.installed && !e.builtIn);
  $: builtInEntries = filtered.filter((e) => e.builtIn && !e.installed);

  async function load(force: boolean) {
    loading = true;
    error = '';
    try {
      const res = await GetVfoxRegistry(force);
      entries = (res?.entries || []) as Entry[];
      registryUrl = res?.registry || '';
      fetchedAt = res?.fetchedAt ? new Date(res.fetchedAt).toLocaleString() : '';
      loaded = true;
    } catch (e) {
      error = $t('plugins.loadError', String(e));
    }
    loading = false;
  }

  async function checkUpdates() {
    checkingUpdates = true;
    try {
      await CheckVfoxPluginUpdates();
      await load(false);
    } catch (e) {
      error = String(e);
    }
    checkingUpdates = false;
  }

  async function toggleDetails(e: Entry) {
    if (expanded === e.name) { expanded = ''; return; }
    expanded = e.name;
    if (details[e.name] === undefined) {
      try {
        details[e.name] = (await GetVfoxPluginManifest(e.name)) as Manifest;
      } catch {
        details[e.name] = null;
      }
      details = details;
    }
  }

  function jobOf(name: string) {
    return $pluginInstalls[name];
  }

  async function install(e: Entry) {
    error = '';
    startPluginJob(e.name, 'install');
    try {
      await InstallVfoxPlugin(e.name);
    } catch (err) {
      error = `${e.displayName}: ${String(err)}`;
      clearPluginJob(e.name);
    }
  }

  async function update(e: Entry) {
    error = '';
    startPluginJob(e.name, 'update');
    try {
      await UpdateVfoxPlugin(e.name);
    } catch (err) {
      error = `${e.displayName}: ${String(err)}`;
      clearPluginJob(e.name);
    }
  }

  async function installFromUrl() {
    const src = sourceUrl.trim();
    if (!src) return;
    error = '';
    startPluginJob(src, 'install');
    try {
      await InstallVfoxPluginFromURL(src);
      sourceUrl = '';
      showUrlInput = false;
    } catch (err) {
      error = String(err);
      clearPluginJob(src);
    }
  }

  // Removal: confirm, and a second, stronger confirmation when versions exist.
  let pendingRemove: Entry | null = null;
  let removing: boolean = false;
  let removeNeedsForce: boolean = false;

  function requestRemove(e: Entry) {
    pendingRemove = e;
    removeNeedsForce = false;
  }

  async function confirmRemove() {
    if (!pendingRemove) return;
    removing = true;
    error = '';
    try {
      await RemoveVfoxPlugin(pendingRemove.name, removeNeedsForce);
      pendingRemove = null;
      await load(false);
      dispatch('changed');
    } catch (err) {
      const msg = String(err);
      if (!removeNeedsForce && /installed version/i.test(msg)) {
        removeNeedsForce = true; // ask again, this time offering to delete versions
      } else {
        error = msg;
        pendingRemove = null;
      }
    }
    removing = false;
  }

  function cancelRemove() {
    if (removing) return;
    pendingRemove = null;
  }

  function hasWindowsNote(m: Manifest | null | undefined): boolean {
    return !!m && (m.notes || []).some((n) => /windows/i.test(n));
  }

  let unsubs: Array<() => void> = [];
  onMount(() => {
    unsubs.push(EventsOn('plugin:installed', (d: any) => {
      load(false);
      if (d?.runtime) dispatch('installed', { name: d.runtime });
    }));
    unsubs.push(EventsOn('runtimes:changed', () => { if (loaded) load(false); }));
  });
  onDestroy(() => { unsubs.forEach((fn) => fn()); unsubs = []; });

  function close() { dispatch('close'); }
</script>

{#if open}
  <div class="fixed inset-0 bg-black/50 flex items-center justify-center z-50" on:click|self={close} on:keydown={(e) => { if (e.key === 'Escape') close(); }} role="dialog" aria-modal="true" tabindex="-1">
    <div class="bg-[var(--color-card)] rounded-xl border border-[var(--color-border)] shadow-2xl w-[680px] max-h-[85vh] flex flex-col">
      <!-- Header -->
      <div class="flex items-start justify-between p-6 pb-4">
        <div>
          <h3 class="text-lg font-bold flex items-center gap-2">
            <svg class="w-4 h-4 text-primary-500" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><line x1="12" y1="8" x2="12" y2="16"/><line x1="8" y1="12" x2="16" y2="12"/></svg>
            {$t('plugins.title')}
          </h3>
          <p class="text-xs text-[var(--color-text-secondary)] mt-1">{$t('plugins.subtitle')}</p>
        </div>
        <button class="btn-icon text-[var(--color-text-secondary)] hover:bg-[var(--color-bg)] shrink-0 ml-3" on:click={close} title={$t('common.close')}>
          <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
        </button>
      </div>

      <!-- Search -->
      <div class="px-6 pb-3">
        <div class="relative">
          <svg class="w-3.5 h-3.5 absolute left-3 top-1/2 -translate-y-1/2 text-[var(--color-text-secondary)]" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/></svg>
          <input
            type="text"
            bind:value={search}
            placeholder={$t('plugins.search')}
            class="w-full pl-9 pr-3 py-2 rounded-lg border border-[var(--color-border)] bg-[var(--color-bg)] text-sm focus:outline-none focus:ring-2 focus:ring-primary-500/50"
          />
        </div>
      </div>

      <!-- Body -->
      <div class="px-6 pb-4 overflow-y-auto flex-1 space-y-5">
        {#if error}
          <div class="p-2.5 rounded-lg bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 text-red-700 dark:text-red-400 text-xs">
            {error}
            <button class="ml-2 underline" on:click={() => error = ''}>{$t('common.dismiss')}</button>
          </div>
        {/if}

        {#if loading && entries.length === 0}
          <div class="text-center py-10">
            <div class="w-7 h-7 border-3 border-primary-500 border-t-transparent rounded-full animate-spin mx-auto mb-3"></div>
            <p class="text-xs text-[var(--color-text-secondary)]">{$t('common.loading')}</p>
          </div>
        {:else if filtered.length === 0 && loaded}
          <div class="text-center py-10 text-[var(--color-text-secondary)] bg-[var(--color-bg)] rounded-xl border border-dashed border-[var(--color-border)]">
            <p class="text-sm">{$t('plugins.empty')}</p>
          </div>
        {:else}
          {#each [
            { key: 'installed', title: $t('plugins.installed'), list: installedEntries },
            { key: 'available', title: $t('plugins.available'), list: availableEntries },
            { key: 'builtin', title: $t('plugins.builtinSection'), list: builtInEntries },
          ] as group (group.key)}
            {#if group.list.length > 0}
              <div>
                <div class="text-[10px] font-bold uppercase tracking-wider text-[var(--color-text-secondary)] mb-1.5 flex items-center gap-2">
                  {group.title}
                  <span class="min-w-[16px] h-[16px] px-1 rounded-full text-[9px] bg-slate-200 dark:bg-slate-700 flex items-center justify-center">{group.list.length}</span>
                </div>
                <div class="space-y-1.5">
                  {#each group.list as e (e.name)}
                    {@const job = jobOf(e.name)}
                    {@const m = details[e.name]}
                    <div class="rounded-lg border {e.builtIn ? 'border-[var(--color-border)] opacity-60' : 'border-[var(--color-border)]'} bg-[var(--color-bg)]/50">
                      <div class="flex items-center justify-between p-2.5">
                        <div class="flex items-center gap-2.5 min-w-0">
                          <div class="w-7 h-7 rounded overflow-hidden shrink-0 p-0.5">{@html runtimeLogo(e.name, e.displayName)}</div>
                          <div class="min-w-0">
                            <div class="flex items-center gap-2 flex-wrap">
                              <span class="text-sm font-semibold">{e.displayName}</span>
                              <span class="font-mono text-[10px] text-[var(--color-text-secondary)]">{e.name}</span>
                              {#if e.kind === 'tool'}
                                <span class="text-[8px] px-1 rounded bg-slate-500/10 text-slate-500 uppercase font-bold">{$t('plugins.kindTool')}</span>
                              {/if}
                              {#if e.installed && e.installedVersion}
                                <span class="text-[9px] px-1 bg-blue-500/10 text-blue-500 rounded border border-blue-500/20 uppercase font-bold">v{e.installedVersion}</span>
                              {/if}
                              {#if e.updateAvailable}
                                <span class="text-[9px] px-1 bg-amber-500/10 text-amber-600 dark:text-amber-400 rounded border border-amber-500/20 uppercase font-bold">→ v{e.updateAvailable}</span>
                              {/if}
                              {#if e.builtIn}
                                <span class="text-[9px] px-1 bg-emerald-500/10 text-emerald-500 rounded border border-emerald-500/20 uppercase font-bold">{$t('plugins.builtinHidden')}</span>
                              {/if}
                              {#if e.installed && e.thirdParty}
                                <span class="text-[9px] px-1 bg-purple-500/10 text-purple-500 rounded border border-purple-500/20 uppercase font-bold" title={$t('plugins.thirdParty')}>{$t('plugins.thirdPartyBadge')}</span>
                              {/if}
                            </div>
                            <p class="text-[11px] text-[var(--color-text-secondary)] truncate" title={e.desc}>
                              {#if job}
                                <span class="text-primary-500">{job.message || (job.action === 'update' ? $t('plugins.updating') : $t('plugins.installing'))}</span>
                              {:else}
                                {e.desc}
                              {/if}
                            </p>
                          </div>
                        </div>
                        <div class="flex items-center gap-1.5 shrink-0 ml-3">
                          {#if !e.builtIn}
                            <button class="btn-icon text-[var(--color-text-secondary)] hover:bg-[var(--color-bg)]" on:click={() => toggleDetails(e)} title={$t('plugins.details')}>
                              <svg class="w-3.5 h-3.5 transition-transform {expanded === e.name ? 'rotate-180' : ''}" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="6 9 12 15 18 9"/></svg>
                            </button>
                          {/if}
                          {#if job && !job.error}
                            <div class="w-4 h-4 border-2 border-primary-500 border-t-transparent rounded-full animate-spin mx-2"></div>
                          {:else if e.builtIn}
                            <span class="text-[10px] text-[var(--color-text-secondary)] px-2">{$t('plugins.builtinAs', e.builtInAs)}</span>
                          {:else if e.installed}
                            {#if e.updateAvailable}
                              <button class="text-xs px-2.5 py-1.5 rounded-lg font-medium bg-amber-500 hover:bg-amber-600 text-white shadow-sm" on:click={() => update(e)} title={$t('plugins.update')}>{$t('common.update')}</button>
                            {/if}
                            <button class="btn-icon text-red-500 hover:bg-red-500/10" on:click={() => requestRemove(e)} title={$t('plugins.remove')}>
                              <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M3 6h18M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/></svg>
                            </button>
                          {:else}
                            <button class="text-xs px-3 py-1.5 rounded-lg font-medium bg-primary-600 hover:bg-primary-700 text-white shadow-sm disabled:opacity-50" on:click={() => install(e)}>{$t('plugins.install')}</button>
                          {/if}
                        </div>
                      </div>
                      {#if job && job.error}
                        <div class="px-2.5 pb-2.5 text-[11px] text-red-600 dark:text-red-400">
                          {job.error}
                          <button class="ml-2 underline" on:click={() => clearPluginJob(e.name)}>{$t('common.dismiss')}</button>
                        </div>
                      {/if}
                      {#if expanded === e.name}
                        <div class="px-3 pb-3 pt-1 border-t border-[var(--color-border)] text-[11px] space-y-1.5">
                          {#if m === undefined}
                            <span class="text-[var(--color-text-secondary)] animate-pulse">{$t('common.loading')}</span>
                          {:else if m === null}
                            <span class="text-[var(--color-text-secondary)]">{$t('plugins.detailsUnavailable')}</span>
                          {:else}
                            <div class="flex flex-wrap gap-x-4 gap-y-1 text-[var(--color-text-secondary)]">
                              {#if m.version}<span>{$t('plugins.pluginVersion', m.version)}</span>{/if}
                              {#if m.license}<span>{$t('plugins.license')}: <span class="text-[var(--color-text)]">{m.license}</span></span>{/if}
                              {#if m.legacyFilenames && m.legacyFilenames.length > 0}<span>{$t('plugins.legacyFiles')}: <span class="font-mono text-[var(--color-text)]">{m.legacyFilenames.join(', ')}</span></span>{/if}
                            </div>
                            {#if e.homepage}
                              <button class="text-primary-500 hover:underline" on:click={() => BrowserOpenURL(e.homepage)}>{e.homepage}</button>
                            {/if}
                            {#if hasWindowsNote(m)}
                              <p class="text-amber-500">{$t('plugins.windowsHint')}</p>
                            {/if}
                            {#if m.notes && m.notes.length > 0}
                              <ul class="list-disc pl-4 space-y-0.5 text-[var(--color-text-secondary)]">
                                {#each m.notes as n}<li>{n}</li>{/each}
                              </ul>
                            {/if}
                            <p class="text-[10px] text-[var(--color-text-secondary)] italic">{$t('plugins.thirdParty')}</p>
                          {/if}
                        </div>
                      {/if}
                    </div>
                  {/each}
                </div>
              </div>
            {/if}
          {/each}
        {/if}
      </div>

      <!-- Footer -->
      <div class="px-6 py-4 border-t border-[var(--color-border)] space-y-3">
        {#if showUrlInput}
          <div class="flex gap-2">
            <input
              type="text"
              bind:value={sourceUrl}
              placeholder={$t('plugins.fromUrlPlaceholder')}
              class="flex-1 px-3 py-2 rounded-lg border border-[var(--color-border)] bg-[var(--color-bg)] text-xs font-mono focus:outline-none focus:ring-2 focus:ring-primary-500/50"
              on:keydown={(e) => { if (e.key === 'Enter') installFromUrl(); }}
            />
            <button class="btn-primary text-xs" on:click={installFromUrl} disabled={!sourceUrl.trim()}>{$t('plugins.install')}</button>
          </div>
          {#if $pluginInstalls[sourceUrl.trim()]}
            <p class="text-[11px] text-primary-500">{$pluginInstalls[sourceUrl.trim()].message || $t('plugins.installing')}</p>
          {/if}
        {/if}
        <div class="flex items-center justify-between">
          <div class="flex items-center gap-3 text-[10px] text-[var(--color-text-secondary)]">
            <button class="hover:text-primary-500 flex items-center gap-1" on:click={() => load(true)} disabled={loading}>
              <svg class="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M23 4v6h-6M1 20v-6h6M3.51 9a9 9 0 0114.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0020.49 15"/></svg>
              {$t('common.refresh')}
            </button>
            {#if installedEntries.length > 0}
              <button class="hover:text-primary-500" on:click={checkUpdates} disabled={checkingUpdates}>{checkingUpdates ? $t('common.loading') : $t('plugins.checkUpdates')}</button>
            {/if}
            <button class="hover:text-primary-500" on:click={() => showUrlInput = !showUrlInput}>{$t('plugins.fromUrl')}</button>
            {#if fetchedAt}<span title={registryUrl}>{$t('runtimes.cachedAt', fetchedAt)}</span>{/if}
          </div>
          <button class="text-xs px-3 py-1.5 rounded-lg font-medium border border-[var(--color-border)] hover:bg-[var(--color-bg)]" on:click={close}>{$t('common.close')}</button>
        </div>
      </div>
    </div>
  </div>
{/if}

<ConfirmDialog
  open={pendingRemove !== null}
  danger={true}
  busy={removing}
  title={pendingRemove ? $t('plugins.remove.confirm', pendingRemove.displayName) : ''}
  message={removeNeedsForce ? $t('plugins.remove.hasVersions') : $t('plugins.remove.msg')}
  confirmLabel={removeNeedsForce ? $t('plugins.removeWithVersions') : $t('plugins.remove')}
  on:confirm={confirmRemove}
  on:cancel={cancelRemove}
/>
