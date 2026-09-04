<script lang="ts">
  import { onMount } from 'svelte';
  import Sidebar from './lib/components/Sidebar.svelte';
  import { currentPage, theme, applyTheme, loadConfig } from './lib/stores/app';
  import { initInstallListeners } from './lib/stores/installs';
  import { initRuntimeCatalog } from './lib/stores/runtimes';
  import { LogFrontendError } from '../wailsjs/go/main/App';

  // Production builds ship without DevTools, so uncaught errors and rejected
  // promises are forwarded to <data>/logs/frontend.log.
  window.addEventListener('error', (e) => {
    try { LogFrontendError('error', String(e.message || e.error || e), String((e.error && e.error.stack) || `${e.filename}:${e.lineno}:${e.colno}`)); } catch {}
  });
  window.addEventListener('unhandledrejection', (e) => {
    const r: any = e.reason;
    try { LogFrontendError('unhandledrejection', String((r && r.message) || r), String((r && r.stack) || '')); } catch {}
  });
  import { initI18n } from './lib/i18n/index';

  // Attach the global install listeners (runtimes + services) once, at app
  // startup, so install progress survives navigating away from and back to the
  // page that started it.
  initInstallListeners();
  // Runtime identity (tabs, labels, logos) comes from the backend catalog.
  initRuntimeCatalog();

  import Dashboard from './pages/Dashboard.svelte';
  import Runtimes from './pages/Runtimes.svelte';
  import Services from './pages/Services.svelte';
  import Tools from './pages/Tools.svelte';
  import Projects from './pages/Projects.svelte';
  import Path from './pages/Path.svelte';
  import Settings from './pages/Settings.svelte';

  const pages: Record<string, any> = {
    dashboard: Dashboard,
    runtimes: Runtimes,
    services: Services,
    tools: Tools,
    projects: Projects,
    path: Path,
    settings: Settings,
  };

  let ready = false;

  onMount(async () => {
    await loadConfig();
    await initI18n();

    // Apply saved theme
    theme.subscribe(t => applyTheme(t));

    ready = true;
  });
</script>

{#if ready}
  <div class="flex h-screen w-screen overflow-hidden">
    <Sidebar />
    <main class="flex-1 overflow-y-auto p-6">
      <svelte:component this={pages[$currentPage] || Dashboard} />
    </main>
  </div>
{:else}
  <div class="flex items-center justify-center h-screen w-screen">
    <div class="text-center">
      <div class="w-10 h-10 rounded-lg bg-primary-500 flex items-center justify-center mx-auto mb-3">
        <span class="text-white font-bold text-lg">D</span>
      </div>
      <p class="text-sm text-[var(--color-text-secondary)]">Loading...</p>
    </div>
  </div>
{/if}
