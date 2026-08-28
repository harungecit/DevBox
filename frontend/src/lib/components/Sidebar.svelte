<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { currentPage } from '../stores/app';
  import { t } from '../i18n/index';
  import AppIcon from './AppIcon.svelte';
  import { GetAppVersion, GetLastUpdateCheck } from '../../../wailsjs/go/main/App';
  import { EventsOn } from '../../../wailsjs/runtime/runtime';

  const navItems = [
    { id: 'dashboard', icon: 'grid', labelKey: 'nav.dashboard' },
    { id: 'runtimes', icon: 'cpu', labelKey: 'nav.runtimes' },
    { id: 'services', icon: 'server', labelKey: 'nav.services' },
    { id: 'tools', icon: 'wrench', labelKey: 'nav.tools' },
    { id: 'projects', icon: 'folder', labelKey: 'nav.projects' },
    { id: 'path', icon: 'terminal', labelKey: 'nav.path' },
    { id: 'settings', icon: 'settings', labelKey: 'nav.settings' },
  ];

  let version = '';
  let updateVersion = '';
  let unsub: (() => void) | null = null;

  onMount(async () => {
    try { version = await GetAppVersion(); } catch { /* ignore */ }
    try { const r = await GetLastUpdateCheck(); if (r?.available) updateVersion = r.latest; } catch { /* ignore */ }
    unsub = EventsOn('appupdate:available', (r: any) => { if (r?.available) updateVersion = r.latest; });
  });
  onDestroy(() => { if (unsub) unsub(); });

  function navigate(page: string) {
    currentPage.set(page);
  }

  // SVG icon paths
  const icons: Record<string, string> = {
    grid: 'M3 3h7v7H3V3zm11 0h7v7h-7V3zm0 11h7v7h-7v-7zM3 14h7v7H3v-7z',
    cpu: 'M9 3v2H6a1 1 0 00-1 1v3H3v2h2v3a1 1 0 001 1h3v2h2v-2h3a1 1 0 001-1v-3h2V9h-2V6a1 1 0 00-1-1h-3V3H9zm0 6h6v6H9V9z',
    server: 'M4 1h16a1 1 0 011 1v4a1 1 0 01-1 1H4a1 1 0 01-1-1V2a1 1 0 011-1zm0 8h16a1 1 0 011 1v4a1 1 0 01-1 1H4a1 1 0 01-1-1v-4a1 1 0 011-1zm0 8h16a1 1 0 011 1v4a1 1 0 01-1 1H4a1 1 0 01-1-1v-4a1 1 0 011-1z',
    folder: 'M2 6a2 2 0 012-2h5l2 2h9a2 2 0 012 2v10a2 2 0 01-2 2H4a2 2 0 01-2-2V6z',
    terminal: 'M4 17l6-5-6-5m8 10h8',
    wrench: 'M14.7 6.3a1 1 0 000 1.4l1.6 1.6a1 1 0 001.4 0l3.77-3.77a6 6 0 01-7.94 7.94l-6.91 6.91a2.12 2.12 0 01-3-3l6.91-6.91a6 6 0 017.94-7.94l-3.76 3.76z',
    settings: 'M12 15a3 3 0 100-6 3 3 0 000 6z',
  };
</script>

<aside class="w-56 h-full flex flex-col bg-[var(--color-sidebar)] border-r border-[var(--color-border)]">
  <!-- Logo -->
  <div class="px-5 py-4 flex items-center gap-3" style="--wails-draggable:drag">
    <div class="w-8 h-8 flex items-center justify-center">
      <AppIcon />
    </div>
    <div>
      <h1 class="text-base font-bold leading-none">DevBox</h1>
      <p class="text-[10px] text-[var(--color-text-secondary)] mt-0.5">v{version}</p>
    </div>
  </div>

  <!-- Navigation -->
  <nav class="flex-1 px-3 py-2 space-y-0.5 overflow-y-auto">
    {#each navItems as item}
      <button
        class="sidebar-item w-full text-left text-[var(--color-text-secondary)]"
        class:active={$currentPage === item.id}
        on:click={() => navigate(item.id)}
      >
        <svg class="w-5 h-5 flex-shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <path d={icons[item.icon] || icons.grid} />
        </svg>
        <span>{$t(item.labelKey)}</span>
      </button>
    {/each}
  </nav>

  <!-- Footer -->
  <div class="px-5 py-3 border-t border-[var(--color-border)]">
    {#if updateVersion}
      <button class="w-full text-left text-[11px] font-bold text-amber-600 dark:text-amber-400 hover:underline flex items-center gap-1.5" on:click={() => navigate('settings')}>
        <span class="w-1.5 h-1.5 rounded-full bg-amber-500 animate-pulse"></span>
        {$t('update.banner', 'v' + updateVersion)}
      </button>
    {:else}
      <p class="text-[11px] text-[var(--color-text-secondary)]">DevBox v{version}</p>
    {/if}
  </div>
</aside>
