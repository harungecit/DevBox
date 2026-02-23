<script lang="ts">
  import { currentPage } from '../stores/app';
  import { t } from '../i18n/index';
  import AppIcon from './AppIcon.svelte';

  const navItems = [
    { id: 'dashboard', icon: 'grid', labelKey: 'nav.dashboard' },
    { id: 'runtimes', icon: 'cpu', labelKey: 'nav.runtimes' },
    { id: 'services', icon: 'server', labelKey: 'nav.services' },
    { id: 'projects', icon: 'folder', labelKey: 'nav.projects' },
    { id: 'path', icon: 'terminal', labelKey: 'nav.path' },
    { id: 'settings', icon: 'settings', labelKey: 'nav.settings' },
  ];

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
      <p class="text-[10px] text-[var(--color-text-secondary)] mt-0.5">v0.1.0</p>
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
    <p class="text-[11px] text-[var(--color-text-secondary)]">DevBox v0.1.0</p>
  </div>
</aside>
