<script lang="ts">
  import { createEventDispatcher, onMount, onDestroy } from 'svelte';
  import { t } from '../i18n/index';
  import { runtimeLogos, serviceLogos } from '../logos';
  import { ScanExternalSoftware, ImportExternalRuntime, ImportExternalService, ImportExternalTool } from '../../../wailsjs/go/main/App';
  import { EventsOn } from '../../../wailsjs/runtime/runtime';
  import { startRuntimeInstall, clearRuntimeInstall, startServiceInstall, clearServiceInstall } from '../stores/installs';

  export let open: boolean = false;
  const dispatch = createEventDispatcher();

  interface Found {
    kind: string;
    name: string;
    displayName: string;
    version: string;
    path: string;
    conflict: string;
  }

  let items: Found[] = [];
  let scanning: boolean = false;
  let scanned: boolean = false;
  let importing: string = ''; // "kind|name|version" of the import in flight
  let error: string = '';

  $: runtimes = items.filter(i => i.kind === 'runtime');
  $: services = items.filter(i => i.kind === 'service');
  $: tools = items.filter(i => i.kind === 'tool');

  // First open triggers a scan; later opens reuse the backend's short cache.
  $: if (open && !scanned && !scanning) scan(false);

  async function scan(force: boolean) {
    scanning = true;
    error = '';
    try {
      items = (await ScanExternalSoftware(force)) || [];
      scanned = true;
    } catch (e) {
      error = String(e);
      items = [];
    }
    scanning = false;
  }

  function keyOf(f: Found): string {
    return `${f.kind}|${f.name}|${f.version}`;
  }

  async function doImport(f: Found) {
    importing = keyOf(f);
    error = '';
    try {
      if (f.kind === 'runtime') {
        startRuntimeInstall(f.name, f.version, true);
        await ImportExternalRuntime(f.name, f.path, f.version);
      } else if (f.kind === 'tool') {
        // Tools link synchronously; the composer:installed event refreshes the list.
        await ImportExternalTool(f.name, f.path);
        importing = '';
        if (scanned) scan(false);
      } else {
        startServiceInstall(f.name, '', true);
        await ImportExternalService(f.name, f.path, f.version);
      }
    } catch (e: any) {
      error = `${f.displayName}: ${typeof e === 'string' ? e : e?.message || e}`;
      if (f.kind === 'runtime') clearRuntimeInstall(f.name);
      else if (f.kind === 'service') clearServiceInstall(f.name);
      importing = '';
    }
  }

  let unsubs: Array<() => void> = [];
  onMount(() => {
    unsubs.push(EventsOn('runtime:installed', (d: any) => {
      if (d?.imported) {
        importing = '';
        if (scanned) scan(false);
      }
    }));
    unsubs.push(EventsOn('service:installed', () => {
      if (importing.startsWith('service|')) importing = '';
      if (scanned) scan(false);
    }));
    unsubs.push(EventsOn('runtime:error', () => {
      if (importing.startsWith('runtime|')) importing = '';
    }));
    unsubs.push(EventsOn('service:error', () => {
      if (importing.startsWith('service|')) importing = '';
    }));
  });
  onDestroy(() => {
    unsubs.forEach(fn => fn());
    unsubs = [];
  });

  function close() {
    dispatch('close');
  }

  function logoFor(f: Found): string {
    if (f.kind === 'tool') return '';
    return (f.kind === 'runtime' ? runtimeLogos[f.name] : serviceLogos[f.name]) || '';
  }
</script>

{#if open}
  <div class="fixed inset-0 bg-black/50 flex items-center justify-center z-50" on:click|self={close} on:keydown={(e) => { if (e.key === 'Escape') close(); }} role="dialog" aria-modal="true" tabindex="-1">
    <div class="bg-[var(--color-card)] rounded-xl border border-[var(--color-border)] shadow-2xl w-[560px] max-h-[80vh] flex flex-col">
      <!-- Header -->
      <div class="flex items-start justify-between p-6 pb-4">
        <div>
          <h3 class="text-lg font-bold flex items-center gap-2">
            <svg class="w-4 h-4 text-primary-500" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/></svg>
            {$t('discovery.centerTitle')}
          </h3>
          <p class="text-xs text-[var(--color-text-secondary)] mt-1">{$t('discovery.centerDesc')}</p>
        </div>
        <button class="btn-icon text-[var(--color-text-secondary)] hover:bg-[var(--color-bg)] shrink-0 ml-3" on:click={close} title={$t('common.dismiss')}>
          <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
        </button>
      </div>

      <!-- Body -->
      <div class="px-6 pb-4 overflow-y-auto flex-1 space-y-5">
        {#if error}
          <div class="p-2.5 rounded-lg bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 text-red-700 dark:text-red-400 text-xs">
            {error}
            <button class="ml-2 underline" on:click={() => error = ''}>{$t('common.dismiss')}</button>
          </div>
        {/if}

        {#if scanning && items.length === 0}
          <div class="text-center py-10">
            <div class="w-7 h-7 border-3 border-primary-500 border-t-transparent rounded-full animate-spin mx-auto mb-3"></div>
            <p class="text-xs text-[var(--color-text-secondary)]">{$t('discovery.scanning')}</p>
          </div>
        {:else if items.length === 0 && scanned}
          <div class="text-center py-10 text-[var(--color-text-secondary)] bg-[var(--color-bg)] rounded-xl border border-dashed border-[var(--color-border)]">
            <svg class="w-10 h-10 mx-auto mb-2 opacity-20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5"><circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/></svg>
            <p class="text-sm">{$t('discovery.empty')}</p>
          </div>
        {:else}
          {#if runtimes.length > 0}
            <div>
              <div class="text-[10px] font-bold uppercase tracking-wider text-[var(--color-text-secondary)] mb-1.5">{$t('nav.runtimes')}</div>
              <div class="space-y-1.5">
                {#each runtimes as f (keyOf(f))}
                  <div class="flex items-center justify-between p-2.5 rounded-lg border border-[var(--color-border)] bg-[var(--color-bg)]/50">
                    <div class="flex items-center gap-2.5 min-w-0">
                      <div class="w-6 h-6 rounded overflow-hidden shrink-0">{@html logoFor(f)}</div>
                      <div class="min-w-0">
                        <div class="flex items-center gap-2">
                          <span class="text-sm font-semibold">{f.displayName}</span>
                          <span class="font-mono text-xs font-bold text-primary-500">{f.version}</span>
                        </div>
                        <p class="font-mono text-[10px] text-[var(--color-text-secondary)] truncate" title={f.path}>{f.path}</p>
                      </div>
                    </div>
                    <button
                      class="text-xs px-3 py-1.5 rounded-lg font-medium bg-primary-600 hover:bg-primary-700 text-white shadow-sm disabled:opacity-50 shrink-0 ml-3"
                      on:click={() => doImport(f)}
                      disabled={importing !== ''}
                      title={$t('discovery.importHint')}
                    >
                      {#if importing === keyOf(f)}
                        <div class="w-3 h-3 border-2 border-white border-t-transparent rounded-full animate-spin inline-block mr-1"></div>
                        {$t('discovery.importing')}
                      {:else}
                        {$t('discovery.import')}
                      {/if}
                    </button>
                  </div>
                {/each}
              </div>
            </div>
          {/if}

          {#if services.length > 0}
            <div>
              <div class="text-[10px] font-bold uppercase tracking-wider text-[var(--color-text-secondary)] mb-1.5">{$t('nav.services')}</div>
              <p class="text-[10px] text-[var(--color-text-secondary)] mb-2">{$t('discovery.servicesNote')}</p>
              <div class="space-y-1.5">
                {#each services as f (keyOf(f))}
                  <div class="flex items-center justify-between p-2.5 rounded-lg border border-[var(--color-border)] bg-[var(--color-bg)]/50">
                    <div class="flex items-center gap-2.5 min-w-0">
                      <div class="w-6 h-6 rounded overflow-hidden shrink-0">{@html logoFor(f)}</div>
                      <div class="min-w-0">
                        <div class="flex items-center gap-2">
                          <span class="text-sm font-semibold">{f.displayName}</span>
                          <span class="font-mono text-xs font-bold text-primary-500">{f.version}</span>
                        </div>
                        <p class="font-mono text-[10px] text-[var(--color-text-secondary)] truncate" title={f.path}>{f.path}</p>
                        {#if f.conflict}
                          <p class="text-[10px] text-amber-500 mt-0.5">{$t('discovery.conflictHint', f.conflict)}</p>
                        {/if}
                      </div>
                    </div>
                    <button
                      class="text-xs px-3 py-1.5 rounded-lg font-medium bg-primary-600 hover:bg-primary-700 text-white shadow-sm disabled:opacity-50 shrink-0 ml-3"
                      on:click={() => doImport(f)}
                      disabled={importing !== '' || f.conflict !== ''}
                      title={f.conflict ? $t('discovery.conflictHint', f.conflict) : $t('discovery.importHint')}
                    >
                      {#if importing === keyOf(f)}
                        <div class="w-3 h-3 border-2 border-white border-t-transparent rounded-full animate-spin inline-block mr-1"></div>
                        {$t('discovery.importing')}
                      {:else}
                        {$t('discovery.import')}
                      {/if}
                    </button>
                  </div>
                {/each}
              </div>
            </div>
          {/if}

          {#if tools.length > 0}
            <div>
              <div class="text-[10px] font-bold uppercase tracking-wider text-[var(--color-text-secondary)] mb-1.5">{$t('discovery.tools')}</div>
              <p class="text-[10px] text-[var(--color-text-secondary)] mb-2">{$t('discovery.toolsNote')}</p>
              <div class="space-y-1.5">
                {#each tools as f (keyOf(f))}
                  <div class="flex items-center justify-between p-2.5 rounded-lg border border-[var(--color-border)] bg-[var(--color-bg)]/50">
                    <div class="flex items-center gap-2.5 min-w-0">
                      <div class="w-6 h-6 rounded bg-orange-500/10 text-orange-500 flex items-center justify-center font-bold text-[11px] shrink-0">{f.displayName.charAt(0)}</div>
                      <div class="min-w-0">
                        <div class="flex items-center gap-2">
                          <span class="text-sm font-semibold">{f.displayName}</span>
                          {#if f.version}
                            <span class="font-mono text-xs font-bold text-primary-500">{f.version}</span>
                          {/if}
                        </div>
                        <p class="font-mono text-[10px] text-[var(--color-text-secondary)] truncate" title={f.path}>{f.path}</p>
                      </div>
                    </div>
                    <button
                      class="text-xs px-3 py-1.5 rounded-lg font-medium bg-primary-600 hover:bg-primary-700 text-white shadow-sm disabled:opacity-50 shrink-0 ml-3"
                      on:click={() => doImport(f)}
                      disabled={importing !== ''}
                      title={$t('discovery.importHint')}
                    >
                      {#if importing === keyOf(f)}
                        <div class="w-3 h-3 border-2 border-white border-t-transparent rounded-full animate-spin inline-block mr-1"></div>
                        {$t('discovery.importing')}
                      {:else}
                        {$t('discovery.import')}
                      {/if}
                    </button>
                  </div>
                {/each}
              </div>
            </div>
          {/if}
        {/if}
      </div>

      <!-- Footer -->
      <div class="flex items-center justify-between p-6 pt-3 border-t border-[var(--color-border)]">
        <button class="text-xs text-primary-500 hover:underline flex items-center gap-1 disabled:opacity-50" on:click={() => scan(true)} disabled={scanning}>
          <svg class="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M23 4v6h-6M1 20v-6h6M3.51 9a9 9 0 0114.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0020.49 15"/></svg>
          {scanning ? $t('discovery.scanning') : $t('discovery.rescan')}
        </button>
        <button class="text-xs px-4 py-2 rounded-lg font-medium border border-[var(--color-border)] hover:bg-[var(--color-bg)] transition-colors" on:click={close}>
          {$t('common.close')}
        </button>
      </div>
    </div>
  </div>
{/if}
