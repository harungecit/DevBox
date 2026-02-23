<script lang="ts">
  import { onMount } from 'svelte';
  import { t } from '../lib/i18n/index';
  import { serviceLogos, runtimeLogos } from '../lib/logos';
  import { GetPATHEntries, AddToPATH, RemoveFromPATH } from '../../wailsjs/go/main/App';

  let pathEntries: string[] = [];
  let newPath: string = '';
  let loading: boolean = true;
  let errorMessage: string = '';

  const devboxPattern = /\.devbox/i;

  $: managedEntries = pathEntries.filter(e => devboxPattern.test(e));
  $: systemEntries = pathEntries.filter(e => !devboxPattern.test(e));

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

  function getRuntimeLabel(path: string): string {
    const p = path.toLowerCase();
    if (p.includes('go')) return 'Go';
    if (p.includes('node')) return 'Node.js';
    if (p.includes('php')) return 'PHP';
    if (p.includes('python')) return 'Python';
    if (p.includes('rust')) return 'Rust';
    if (p.includes('nginx')) return 'Nginx';
    if (p.includes('postgres')) return 'PostgreSQL';
    if (p.includes('mysql')) return 'MySQL';
    if (p.includes('mariadb')) return 'MariaDB';
    if (p.includes('mongodb')) return 'MongoDB';
    if (p.includes('redis')) return 'Redis';
    if (p.includes('valkey')) return 'Valkey';
    if (p.includes('mailpit')) return 'Mailpit';
    return 'DevBox';
  }

  function getIcon(path: string): string {
    const p = path.toLowerCase();
    if (p.includes('go')) return runtimeLogos.go;
    if (p.includes('node')) return runtimeLogos.node;
    if (p.includes('php')) return runtimeLogos.php;
    if (p.includes('python')) return runtimeLogos.python;
    if (p.includes('rust')) return runtimeLogos.rust;
    if (p.includes('nginx')) return serviceLogos.nginx;
    if (p.includes('postgres')) return serviceLogos.postgres;
    if (p.includes('mysql')) return serviceLogos.mysql;
    if (p.includes('mariadb')) return serviceLogos.mariadb;
    if (p.includes('mongodb')) return serviceLogos.mongodb;
    if (p.includes('redis')) return serviceLogos.redis;
    if (p.includes('valkey')) return serviceLogos.valkey;
    if (p.includes('mailpit')) return serviceLogos.mailpit;
    return '';
  }

  onMount(loadPATH);
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
          <div class="flex items-center justify-between p-3 rounded-xl border border-slate-100 dark:border-slate-800 hover:bg-slate-50 dark:hover:bg-slate-800/50 transition-all group">
            <div class="flex items-center gap-4 flex-1 min-w-0">
              <div class="w-8 h-8 rounded-lg overflow-hidden flex-shrink-0 shadow-sm bg-white dark:bg-slate-700 p-1">
                {@html getIcon(entry) || ''}
              </div>
              <div class="truncate">
                <p class="text-xs font-bold text-[var(--color-text)]">{getRuntimeLabel(entry)}</p>
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

  <!-- System PATH (read-only) -->
  <div class="card">
    <div class="mb-4">
      <h3 class="text-sm font-semibold flex items-center gap-2">
        <svg class="w-4 h-4 text-surface-500" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <rect x="3" y="11" width="18" height="11" rx="2" ry="2"/><path d="M7 11V7a5 5 0 0 1 10 0v4"/>
        </svg>
        {$t('path.system')}
        <span class="badge badge-info">{systemEntries.length}</span>
      </h3>
      <p class="text-xs text-[var(--color-text-secondary)] mt-1">{$t('path.systemDesc')}</p>
    </div>

    {#if systemEntries.length > 0}
      <div class="space-y-1 max-h-64 overflow-y-auto">
        {#each systemEntries as entry}
          <div class="flex items-center p-2 rounded-lg">
            <span class="font-mono text-xs text-[var(--color-text-secondary)] truncate">{entry}</span>
          </div>
        {/each}
      </div>
    {:else if loading}
      <p class="text-sm text-[var(--color-text-secondary)] text-center py-4">{$t('common.loading')}</p>
    {/if}
  </div>
</div>
