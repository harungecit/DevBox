<script lang="ts">
  import { t } from '../lib/i18n/index';
  import StatusBadge from '../lib/components/StatusBadge.svelte';
  import { currentPage } from '../lib/stores/app';
  import { serviceLogos, runtimeLogos } from '../lib/logos';
  import {
    GetInstalledRuntimes,
    GetAllServices,
    GetGlobalRuntime,
    StartService,
    StopService,
    GetProxyStatus,
    InstallProxy,
    StartProxy,
    StopProxy,
  } from '../../wailsjs/go/main/App';
  import { EventsOn } from '../../wailsjs/runtime/runtime';
  import { onMount, onDestroy } from 'svelte';

  interface ServiceInfo {
    name: string;
    displayName: string;
    status: string;
    port: number;
    version: string;
    installed: boolean;
  }

  let runtimes: Record<string, string[]> = {};
  let services: Record<string, ServiceInfo> = {};
  let activeVersions: Record<string, string> = {};
  let loading = true;
  let togglingService: string = '';

  interface ProxyStatus {
    installed: boolean;
    running: boolean;
    enabled: boolean;
    port: number;
  }
  let proxyStatus: ProxyStatus = { installed: false, running: false, enabled: false, port: 80 };
  let proxyBusy: boolean = false;
  let proxyError: string = '';

  async function loadProxyStatus() {
    try {
      proxyStatus = await GetProxyStatus();
    } catch (e) {
      console.error('proxy status load error:', e);
    }
  }

  async function installProxyHandler() {
    proxyBusy = true;
    proxyError = '';
    try {
      await InstallProxy();
      await loadProxyStatus();
    } catch (e: any) {
      proxyError = String(e?.message || e);
    }
    proxyBusy = false;
  }

  async function startProxyHandler() {
    proxyBusy = true;
    proxyError = '';
    try {
      await StartProxy();
      await loadProxyStatus();
    } catch (e: any) {
      proxyError = String(e?.message || e);
    }
    proxyBusy = false;
  }

  async function stopProxyHandler() {
    proxyBusy = true;
    proxyError = '';
    try {
      await StopProxy();
      await loadProxyStatus();
    } catch (e: any) {
      proxyError = String(e?.message || e);
    }
    proxyBusy = false;
  }

  const runtimeLabels: Record<string, string> = {
    go: 'Go', node: 'Node.js', php: 'PHP', python: 'Python', rust: 'Rust'
  };

  async function loadData() {
    try {
      runtimes = await GetInstalledRuntimes() || {};
      services = await GetAllServices() || {};

      for (const rt of Object.keys(runtimes)) {
        activeVersions[rt] = await GetGlobalRuntime(rt);
      }
    } catch (e) {
      console.error('Dashboard load error:', e);
    } finally {
      loading = false;
    }
  }

  // Live status: react to backend events (auto-start, tray start/stop-all,
  // installs) and poll while the page is open so state never looks stale.
  let unsubs: Array<() => void> = [];
  let pollTimer: ReturnType<typeof setInterval> | null = null;

  onMount(async () => {
    await loadData();
    await loadProxyStatus();
    unsubs.push(EventsOn('services:changed', () => { loadData(); }));
    unsubs.push(EventsOn('service:installed', () => { loadData(); }));
    unsubs.push(EventsOn('runtime:installed', () => { loadData(); }));
    pollTimer = setInterval(() => { loadData(); loadProxyStatus(); }, 5000);
  });

  onDestroy(() => {
    unsubs.forEach(fn => fn());
    if (pollTimer) clearInterval(pollTimer);
  });

  $: installedServices = Object.values(services).filter(s => s.installed);
  $: runningServices = installedServices.filter(s => s.status === 'running');
  $: runtimeEntries = Object.entries(runtimes);

  async function toggleService(svc: ServiceInfo) {
    togglingService = svc.name;
    try {
      if (svc.status === 'running') {
        await StopService(svc.name.toLowerCase());
      } else {
        await StartService(svc.name.toLowerCase());
      }
      await loadData();
    } catch (e) {
      console.error('Service toggle error:', e);
    } finally {
      togglingService = '';
    }
  }
</script>

<div class="space-y-6">
  <!-- Header -->
  <div class="flex items-center justify-between">
    <div>
      <h2 class="text-2xl font-bold">{$t('dashboard.title')}</h2>
      <p class="text-[var(--color-text-secondary)] mt-1">{$t('dashboard.welcome')}</p>
    </div>
    <button class="btn-secondary text-xs" on:click={loadData}>
      <svg class="w-3.5 h-3.5 mr-1" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M23 4v6h-6M1 20v-6h6M3.51 9a9 9 0 0114.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0020.49 15"/></svg>
      {$t('common.refresh')}
    </button>
  </div>

  <!-- Front-door proxy status -->
  <div class="card p-5">
    <div class="flex items-start justify-between gap-4">
      <div class="flex items-start gap-3 min-w-0">
        <div class="w-10 h-10 rounded-lg {proxyStatus.running ? 'bg-emerald-500/10 text-emerald-500' : 'bg-slate-200 dark:bg-slate-700 text-slate-500'} flex items-center justify-center font-bold text-sm flex-shrink-0">
          <svg class="w-5 h-5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><path d="M2 12h20M12 2a15.3 15.3 0 0 1 4 10 15.3 15.3 0 0 1-4 10 15.3 15.3 0 0 1-4-10 15.3 15.3 0 0 1 4-10z"/></svg>
        </div>
        <div class="min-w-0">
          <div class="flex items-center gap-2 flex-wrap">
            <h3 class="font-bold text-base">{$t('dashboard.proxyTitle')}</h3>
            {#if proxyStatus.running}
              <span class="text-[10px] px-1.5 py-0.5 bg-emerald-500/10 text-emerald-500 rounded border border-emerald-500/20 font-bold uppercase">{$t('dashboard.proxyRunningOnPort', proxyStatus.port)}</span>
            {:else if proxyStatus.installed}
              <span class="text-[10px] px-1.5 py-0.5 bg-slate-500/10 text-slate-500 rounded border border-slate-500/20 font-bold uppercase">{$t('dashboard.proxyStopped')}</span>
            {:else}
              <span class="text-[10px] px-1.5 py-0.5 bg-amber-500/10 text-amber-500 rounded border border-amber-500/20 font-bold uppercase">{$t('dashboard.proxyNotInstalled')}</span>
            {/if}
          </div>
          <p class="text-xs text-[var(--color-text-secondary)] mt-1">{$t('dashboard.proxyDesc')}</p>
          {#if !proxyStatus.installed}
            <p class="text-[11px] text-[var(--color-text-secondary)] italic mt-1">{$t('dashboard.proxyAdminHint')}</p>
          {/if}
          {#if proxyError}
            <p class="text-[11px] text-red-500 mt-1 font-mono">{proxyError}</p>
          {/if}
        </div>
      </div>
      <div class="flex items-center gap-2 flex-shrink-0">
        {#if !proxyStatus.installed}
          <button
            class="text-xs px-3 py-1.5 rounded-lg bg-blue-600 text-white hover:bg-blue-700 disabled:opacity-50 flex items-center gap-1.5"
            on:click={installProxyHandler}
            disabled={proxyBusy}
          >
            {#if proxyBusy}<div class="w-3 h-3 border-2 border-white border-t-transparent rounded-full animate-spin"></div>{/if}
            {$t('dashboard.proxyInstall')}
          </button>
        {:else if proxyStatus.running}
          <button
            class="text-xs px-3 py-1.5 rounded-lg border border-[var(--color-border)] hover:bg-[var(--color-bg)] disabled:opacity-50"
            on:click={stopProxyHandler}
            disabled={proxyBusy}
          >
            {$t('dashboard.proxyStop')}
          </button>
        {:else}
          <button
            class="text-xs px-3 py-1.5 rounded-lg bg-emerald-600 text-white hover:bg-emerald-700 disabled:opacity-50 flex items-center gap-1.5"
            on:click={startProxyHandler}
            disabled={proxyBusy}
          >
            {#if proxyBusy}<div class="w-3 h-3 border-2 border-white border-t-transparent rounded-full animate-spin"></div>{/if}
            {$t('dashboard.proxyStart')}
          </button>
        {/if}
      </div>
    </div>
  </div>

  <!-- Main Content Grid -->
  <div class="grid grid-cols-1 lg:grid-cols-2 gap-6">
    <!-- Services Panel -->
    <div class="card !p-0 overflow-hidden">
      <div class="flex items-center justify-between px-5 py-4 border-b border-[var(--color-border)]">
        <div class="flex items-center gap-2.5">
          <div class="w-2 h-2 rounded-full {runningServices.length > 0 ? 'bg-emerald-500 shadow-[0_0_6px_rgba(16,185,129,0.4)]' : 'bg-slate-400'}"></div>
          <h3 class="font-bold text-sm">{$t('dashboard.services')}</h3>
        </div>
        <button class="text-xs text-primary-500 hover:underline font-medium" on:click={() => currentPage.set('services')}>
          {$t('dashboard.manage')}
        </button>
      </div>

      {#if installedServices.length > 0}
        <div class="divide-y divide-[var(--color-border)]">
          {#each installedServices as svc}
            <div class="flex items-center justify-between px-5 py-3 hover:bg-[var(--color-bg)]/50 transition-colors">
              <div class="flex items-center gap-3">
                <div class="w-8 h-8 rounded-lg overflow-hidden flex-shrink-0">
                  {@html serviceLogos[svc.name?.toLowerCase()] || ''}
                </div>
                <div>
                  <p class="text-sm font-semibold leading-tight">{svc.displayName}</p>
                  <div class="flex items-center gap-2 mt-0.5">
                    <span class="text-[10px] font-mono text-[var(--color-text-secondary)]">:{svc.port}</span>
                    {#if svc.version}
                      <span class="text-[10px] text-[var(--color-text-secondary)]">v{svc.version}</span>
                    {/if}
                  </div>
                </div>
              </div>
              <div class="flex items-center gap-2.5">
                <StatusBadge
                  status={svc.status === 'running' ? 'running' : 'stopped'}
                  label={svc.status === 'running' ? $t('services.running') : $t('services.stopped')}
                />
                <button
                  class="w-8 h-8 rounded-lg flex items-center justify-center transition-all {svc.status === 'running' ? 'bg-red-500/10 text-red-500 hover:bg-red-500/20' : 'bg-emerald-500/10 text-emerald-500 hover:bg-emerald-500/20'}"
                  on:click={() => toggleService(svc)}
                  disabled={togglingService !== ''}
                >
                  {#if togglingService === svc.name}
                    <div class="w-3.5 h-3.5 border-2 border-current border-t-transparent rounded-full animate-spin"></div>
                  {:else if svc.status === 'running'}
                    <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="currentColor"><rect x="6" y="6" width="12" height="12" rx="1"/></svg>
                  {:else}
                    <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="currentColor"><path d="M6 4l15 8-15 8V4z"/></svg>
                  {/if}
                </button>
              </div>
            </div>
          {/each}
        </div>
      {:else}
        <div class="text-center py-12 px-6">
          <div class="w-12 h-12 rounded-xl bg-slate-100 dark:bg-slate-800 flex items-center justify-center mx-auto mb-3">
            <svg class="w-6 h-6 text-slate-400" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
              <path d="M4 1h16a1 1 0 011 1v4a1 1 0 01-1 1H4a1 1 0 01-1-1V2a1 1 0 011-1zm0 8h16a1 1 0 011 1v4a1 1 0 01-1 1H4a1 1 0 01-1-1v-4a1 1 0 011-1z"/>
            </svg>
          </div>
          <p class="text-sm text-[var(--color-text-secondary)]">{$t('dashboard.noServicesInstalled')}</p>
          <button class="text-xs text-primary-500 mt-2 hover:underline font-medium" on:click={() => currentPage.set('services')}>
            {$t('dashboard.goToServices')}
          </button>
        </div>
      {/if}
    </div>

    <!-- Runtimes Panel -->
    <div class="card !p-0 overflow-hidden">
      <div class="flex items-center justify-between px-5 py-4 border-b border-[var(--color-border)]">
        <div class="flex items-center gap-2.5">
          <div class="w-2 h-2 rounded-full {runtimeEntries.length > 0 ? 'bg-blue-500 shadow-[0_0_6px_rgba(59,130,246,0.4)]' : 'bg-slate-400'}"></div>
          <h3 class="font-bold text-sm">{$t('dashboard.runtimes')}</h3>
        </div>
        <button class="text-xs text-primary-500 hover:underline font-medium" on:click={() => currentPage.set('runtimes')}>
          {$t('dashboard.manage')}
        </button>
      </div>

      {#if runtimeEntries.length > 0}
        <div class="divide-y divide-[var(--color-border)]">
          {#each runtimeEntries as [name, versions]}
            <div class="flex items-center justify-between px-5 py-3 hover:bg-[var(--color-bg)]/50 transition-colors">
              <div class="flex items-center gap-3">
                <div class="w-8 h-8 rounded-lg overflow-hidden flex-shrink-0">
                  {@html runtimeLogos[name] || ''}
                </div>
                <div>
                  <p class="text-sm font-semibold leading-tight">{runtimeLabels[name] || name}</p>
                </div>
              </div>
              <div class="flex items-center gap-2">
                {#if activeVersions[name]}
                  <div class="flex items-center gap-1.5 px-2.5 py-1 rounded-lg bg-primary-500/10 border border-primary-500/20">
                    <span class="text-[10px] font-bold text-primary-400 uppercase">{$t('dashboard.activeVersion')}</span>
                    <span class="text-xs font-mono font-bold text-primary-500">{activeVersions[name]}</span>
                  </div>
                {:else}
                  <span class="text-[10px] text-[var(--color-text-secondary)] px-2.5 py-1 rounded-lg border border-dashed border-[var(--color-border)]">
                    {$t('dashboard.none')}
                  </span>
                {/if}
              </div>
            </div>
          {/each}
        </div>
      {:else}
        <div class="text-center py-12 px-6">
          <div class="w-12 h-12 rounded-xl bg-slate-100 dark:bg-slate-800 flex items-center justify-center mx-auto mb-3">
            <svg class="w-6 h-6 text-slate-400" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
              <path d="M16 18l6-6-6-6M8 6l-6 6 6 6"/>
            </svg>
          </div>
          <p class="text-sm text-[var(--color-text-secondary)]">{$t('dashboard.noRuntimesInstalled')}</p>
          <button class="text-xs text-primary-500 mt-2 hover:underline font-medium" on:click={() => currentPage.set('runtimes')}>
            {$t('dashboard.goToRuntimes')}
          </button>
        </div>
      {/if}
    </div>
  </div>

  <!-- Quick Actions -->
  <div class="grid grid-cols-3 gap-3">
    <button class="group flex items-center gap-3 p-4 rounded-xl border border-[var(--color-border)] bg-[var(--color-card)] hover:border-blue-500/30 hover:bg-blue-500/5 transition-all" on:click={() => currentPage.set('runtimes')}>
      <div class="w-9 h-9 rounded-lg bg-blue-500/10 flex items-center justify-center group-hover:bg-blue-500/20 transition-colors flex-shrink-0">
        <svg class="w-4.5 h-4.5 text-blue-500" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/></svg>
      </div>
      <span class="text-sm font-semibold">{$t('dashboard.installRuntime')}</span>
    </button>

    <button class="group flex items-center gap-3 p-4 rounded-xl border border-[var(--color-border)] bg-[var(--color-card)] hover:border-amber-500/30 hover:bg-amber-500/5 transition-all" on:click={() => currentPage.set('projects')}>
      <div class="w-9 h-9 rounded-lg bg-amber-500/10 flex items-center justify-center group-hover:bg-amber-500/20 transition-colors flex-shrink-0">
        <svg class="w-4.5 h-4.5 text-amber-500" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><path d="M12 20h9M16.5 3.5a2.121 2.121 0 0 1 3 3L7 19l-4 1 1-4L16.5 3.5z"/></svg>
      </div>
      <span class="text-sm font-semibold">{$t('dashboard.addProject')}</span>
    </button>

    <button class="group flex items-center gap-3 p-4 rounded-xl border border-[var(--color-border)] bg-[var(--color-card)] hover:border-emerald-500/30 hover:bg-emerald-500/5 transition-all" on:click={() => currentPage.set('services')}>
      <div class="w-9 h-9 rounded-lg bg-emerald-500/10 flex items-center justify-center group-hover:bg-emerald-500/20 transition-colors flex-shrink-0">
        <svg class="w-4.5 h-4.5 text-emerald-500" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><path d="M13 2L3 14h9l-1 8 10-12h-9l1-8z"/></svg>
      </div>
      <span class="text-sm font-semibold">{$t('dashboard.startAllServices')}</span>
    </button>
  </div>
</div>
