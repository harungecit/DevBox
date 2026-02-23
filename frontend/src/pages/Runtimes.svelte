<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { t } from '../lib/i18n/index';
  import StatusBadge from '../lib/components/StatusBadge.svelte';
  import ProgressBar from '../lib/components/ProgressBar.svelte';
  import { runtimeLogos } from '../lib/logos';
  import {
    GetRemoteVersions,
    GetInstalledVersions,
    InstallRuntime,
    UninstallRuntime,
    SetGlobalRuntime,
    GetGlobalRuntime,
    GetPHPExtensions,
    TogglePHPExtension,
    IsComposerInstalled,
    InstallComposer,
    GetComposerVersion,
    StartPHPCGI,
    StopPHPCGI,
    IsPHPCGIRunning,
    IsBunInstalled,
    InstallBun,
    GetBunVersion,
    UninstallBun,
  } from '../../wailsjs/go/main/App';
  import { EventsOn, EventsOff } from '../../wailsjs/runtime/runtime';

  interface VersionInfo {
    number: string;
    stable: boolean;
    current: boolean;
    installed: boolean;
  }

  interface PHPExtension {
    name: string;
    enabled: boolean;
  }

  let activeTab: string = 'go';
  let installedVersions: VersionInfo[] = [];
  let remoteVersions: VersionInfo[] = [];
  let loadingRemote: boolean = false;
  let loadingInstalled: boolean = false;
  let installing: string = ''; // version being installed
  let progressPercent: number = 0;
  let progressMessage: string = '';
  let errorMessage: string = '';

  // PHP-specific state
  let phpExtensions: PHPExtension[] = [];
  let loadingExtensions: boolean = false;
  let composerInstalled: boolean = false;
  let composerVersion: string = '';
  let installingComposer: boolean = false;
  let phpCgiRunning: boolean = false;
  let phpCgiPort: number = 9000;
  let extensionFilter: string = '';

  // Bun state
  let bunInstalled: boolean = false;
  let bunVersion: string = '';
  let installingBun: boolean = false;
  let uninstallingBun: boolean = false;

  const tabs = [
    { id: 'go', label: 'Go' },
    { id: 'node', label: 'Node.js' },
    { id: 'php', label: 'PHP' },
    { id: 'python', label: 'Python' },
    { id: 'rust', label: 'Rust' },
  ];

  async function loadInstalled() {
    loadingInstalled = true;
    try {
      const result = await GetInstalledVersions(activeTab);
      installedVersions = result || [];
    } catch (e) {
      console.error('Failed to load installed:', e);
      installedVersions = [];
    }
    loadingInstalled = false;
  }

  async function loadRemote() {
    loadingRemote = true;
    errorMessage = '';
    try {
      const result = await GetRemoteVersions(activeTab);
      remoteVersions = result || [];
    } catch (e) {
      console.error('Failed to load remote:', e);
      errorMessage = String(e);
      remoteVersions = [];
    }
    loadingRemote = false;
  }

  async function switchTab(tab: string) {
    activeTab = tab;
    installing = '';
    progressPercent = 0;
    progressMessage = '';
    errorMessage = '';
    await Promise.all([loadInstalled(), loadRemote()]);
    if (tab === 'php') {
      await loadPHPExtras();
    }
    if (tab === 'node') {
      await loadBunStatus();
    }
  }

  async function loadPHPExtras() {
    // Find the active PHP version
    const active = installedVersions.find(v => v.current);
    if (active) {
      await loadExtensions(active.number);
    }
    try {
      composerInstalled = await IsComposerInstalled();
      if (composerInstalled) {
        composerVersion = await GetComposerVersion();
      }
      phpCgiRunning = await IsPHPCGIRunning();
    } catch (e) {
      // ignore
    }
  }

  async function loadExtensions(version: string) {
    loadingExtensions = true;
    try {
      phpExtensions = await GetPHPExtensions(version) || [];
    } catch (e) {
      phpExtensions = [];
    }
    loadingExtensions = false;
  }

  async function toggleExtension(ext: PHPExtension) {
    const active = installedVersions.find(v => v.current);
    if (!active) return;
    try {
      await TogglePHPExtension(active.number, ext.name, !ext.enabled);
      ext.enabled = !ext.enabled;
      phpExtensions = [...phpExtensions];
    } catch (e) {
      errorMessage = String(e);
    }
  }

  async function installComposer() {
    installingComposer = true;
    try {
      await InstallComposer();
    } catch (e) {
      errorMessage = String(e);
      installingComposer = false;
    }
  }

  async function togglePhpCgi() {
    const active = installedVersions.find(v => v.current);
    if (!active) return;
    try {
      if (phpCgiRunning) {
        await StopPHPCGI();
      } else {
        await StartPHPCGI(active.number, phpCgiPort);
      }
      phpCgiRunning = await IsPHPCGIRunning();
    } catch (e) {
      errorMessage = String(e);
    }
  }

  async function loadBunStatus() {
    try {
      bunInstalled = await IsBunInstalled();
      if (bunInstalled) {
        bunVersion = await GetBunVersion();
      }
    } catch (e) {
      // ignore
    }
  }

  async function installBun() {
    installingBun = true;
    errorMessage = '';
    try {
      await InstallBun();
    } catch (e) {
      errorMessage = String(e);
      installingBun = false;
    }
  }

  async function uninstallBun() {
    uninstallingBun = true;
    try {
      await UninstallBun();
      bunInstalled = false;
      bunVersion = '';
    } catch (e) {
      errorMessage = String(e);
    }
    uninstallingBun = false;
  }

  async function install(version: string) {
    installing = version;
    progressPercent = 0;
    progressMessage = '';
    errorMessage = '';
    try {
      await InstallRuntime(activeTab, version);
      await Promise.all([loadInstalled(), loadRemote()]);
    } catch (e) {
      errorMessage = String(e);
    }
    installing = '';
  }

  async function uninstall(version: string) {
    try {
      await UninstallRuntime(activeTab, version);
      await Promise.all([loadInstalled(), loadRemote()]);
    } catch (e) {
      errorMessage = String(e);
    }
  }

  async function setGlobal(version: string) {
    try {
      await SetGlobalRuntime(activeTab, version);
      await loadInstalled();
    } catch (e) {
      errorMessage = String(e);
    }
  }

  // Listen for progress events from Go backend
  function onProgress(data: any) {
    if (data.name === activeTab && data.version === installing) {
      progressPercent = data.percent;
      progressMessage = data.message;
    }
  }

  function onError(data: any) {
    if (data.name === activeTab) {
      errorMessage = data.error;
      installing = '';
    }
  }

  onMount(() => {
    EventsOn('runtime:progress', onProgress);
    EventsOn('runtime:error', onError);
    EventsOn('runtime:installed', () => {
      loadInstalled();
      loadRemote();
    });
    EventsOn('composer:installed', () => {
      installingComposer = false;
      composerInstalled = true;
      GetComposerVersion().then(v => composerVersion = v);
    });
    EventsOn('composer:error', (data: any) => {
      installingComposer = false;
      errorMessage = data.error;
    });
    EventsOn('bun:installed', () => {
      installingBun = false;
      bunInstalled = true;
      GetBunVersion().then(v => bunVersion = v);
    });
    EventsOn('bun:error', (data: any) => {
      installingBun = false;
      errorMessage = data.error;
    });
    loadInstalled();
    loadRemote();
  });

  onDestroy(() => {
    EventsOff('runtime:progress');
    EventsOff('runtime:error');
    EventsOff('runtime:installed');
    EventsOff('composer:installed');
    EventsOff('composer:error');
    EventsOff('bun:installed');
    EventsOff('bun:error');
  });
</script>

<div class="space-y-6">
  <div>
    <h2 class="text-2xl font-bold">{$t('runtimes.title')}</h2>
    <p class="text-[var(--color-text-secondary)] mt-1">{$t('runtimes.subtitle')}</p>
  </div>

  <!-- Runtime Tabs -->
  <div class="flex gap-1 border-b border-[var(--color-border)]">
    {#each tabs as tab}
      <button
        class="px-4 py-2.5 text-sm font-medium transition-colors border-b-2 -mb-px flex items-center gap-2"
        class:border-primary-500={activeTab === tab.id}
        class:text-primary-500={activeTab === tab.id}
        class:border-transparent={activeTab !== tab.id}
        class:text-[var(--color-text-secondary)]={activeTab !== tab.id}
        on:click={() => switchTab(tab.id)}
        disabled={installing !== ''}
      >
        <div class="w-5 h-5 rounded overflow-hidden">{@html runtimeLogos[tab.id] || ''}</div>
        {tab.label}
      </button>
    {/each}
  </div>

  <!-- Error message -->
  {#if errorMessage}
    <div class="p-3 rounded-lg bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 text-red-700 dark:text-red-400 text-sm">
      {errorMessage}
      <button class="ml-2 underline" on:click={() => errorMessage = ''}>{$t('common.dismiss')}</button>
    </div>
  {/if}

  <!-- Progress bar when installing -->
  {#if installing}
    <div class="card">
      <div class="flex items-center gap-3 mb-2">
        <div class="w-2 h-2 rounded-full bg-primary-500 animate-pulse"></div>
        <span class="text-sm font-medium">{$t('runtimes.downloading')} {activeTab} {installing}</span>
      </div>
      <ProgressBar percent={progressPercent} message={progressMessage} />
    </div>
  {/if}

  <div class="space-y-6">
    <!-- Installed Versions -->
    <div class="card p-5">
      <div class="flex items-center justify-between mb-4">
        <h3 class="text-base font-bold flex items-center gap-2">
          {$t('runtimes.installed')}
          <span class="badge badge-info">{installedVersions.length}</span>
        </h3>
        {#if loadingInstalled}
          <span class="text-xs text-[var(--color-text-secondary)] animate-pulse">{$t('common.loading')}</span>
        {/if}
      </div>

      {#if installedVersions.length > 0}
        <div class="space-y-2">
          {#each installedVersions as ver}
            <div class="flex items-center justify-between p-4 rounded-xl border border-[var(--color-border)] hover:bg-[var(--color-bg)] transition-colors">
              <div class="flex items-center gap-4">
                <div class="w-8 h-8 rounded-lg overflow-hidden shadow-sm">
                  {@html runtimeLogos[activeTab] || ''}
                </div>
                <span class="font-mono text-sm font-semibold">{ver.number}</span>
                {#if ver.current}
                  <StatusBadge status="active" label={$t('runtimes.active')} />
                {/if}
              </div>
              <div class="flex gap-2">
                {#if !ver.current}
                  <button 
                    class="btn-icon bg-emerald-600 hover:bg-emerald-700 text-white shadow-sm" 
                    on:click={() => setGlobal(ver.number)} 
                    disabled={installing !== ''}
                    title={$t('runtimes.setGlobal')}
                  >
                    <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><polyline points="20 6 9 17 4 12"/></svg>
                  </button>
                {/if}
                <button
                  class="btn-icon text-red-500 hover:bg-red-500/10"
                  on:click={() => uninstall(ver.number)}
                  disabled={installing !== ''}
                  title={$t('runtimes.uninstall')}
                >
                  <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M3 6h18M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/></svg>
                </button>
              </div>
            </div>
          {/each}
        </div>
      {:else if !loadingInstalled}
        <div class="text-center py-10 text-[var(--color-text-secondary)] bg-[var(--color-bg)] rounded-xl border border-dashed border-[var(--color-border)]">
          <svg class="w-12 h-12 mx-auto mb-3 opacity-20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
            <path d="M20 7l-8-4-8 4m16 0l-8 4m8-4v10l-8 4m0-10L4 7m8 4v10M4 7v10l8 4" />
          </svg>
          <p class="text-sm font-medium">{$t('runtimes.emptyInstalled')}</p>
          <p class="text-xs mt-1 opacity-60">{$t('runtimes.emptyInstalledHint')}</p>
        </div>
      {/if}
    </div>

    <!-- PHP Extras (only when PHP tab is active and has installed versions) -->
    {#if activeTab === 'php' && installedVersions.some(v => v.current)}
      <!-- PHP Extensions -->
      <div class="card p-5">
        <div class="flex items-center justify-between mb-3">
          <h3 class="text-base font-bold">{$t('runtimes.phpExtensions')}</h3>
          <input
            type="text"
            bind:value={extensionFilter}
            placeholder="Filter..."
            class="px-2 py-1 text-xs rounded-lg border border-[var(--color-border)] bg-[var(--color-bg)] w-40"
          />
        </div>
        <p class="text-xs text-[var(--color-text-secondary)] mb-3">{$t('runtimes.phpExtensionsDesc')}</p>
        {#if loadingExtensions}
          <span class="text-xs text-[var(--color-text-secondary)] animate-pulse">{$t('common.loading')}</span>
        {:else if phpExtensions.length > 0}
          <div class="grid grid-cols-2 md:grid-cols-3 gap-1.5 max-h-[240px] overflow-y-auto pr-1">
            {#each phpExtensions.filter(e => !extensionFilter || e.name.toLowerCase().includes(extensionFilter.toLowerCase())) as ext}
              <button
                class="flex items-center justify-between px-2.5 py-1.5 rounded-lg border transition-all text-xs {ext.enabled ? 'border-emerald-500/30 bg-emerald-500/5' : 'border-[var(--color-border)] bg-[var(--color-bg)]'}"
                on:click={() => toggleExtension(ext)}
              >
                <span class="font-mono truncate">{ext.name}</span>
                <span class="text-[10px] font-bold ml-1 {ext.enabled ? 'text-emerald-500' : 'text-slate-400'}">
                  {ext.enabled ? $t('runtimes.enabled') : $t('runtimes.disabled')}
                </span>
              </button>
            {/each}
          </div>
        {:else}
          <p class="text-xs text-[var(--color-text-secondary)]">No extensions found in php.ini</p>
        {/if}
      </div>

      <div class="grid grid-cols-2 gap-4">
        <!-- Composer Card -->
        <div class="card p-5">
          <h3 class="text-base font-bold mb-1">{$t('runtimes.composer')}</h3>
          <p class="text-xs text-[var(--color-text-secondary)] mb-3">{$t('runtimes.composerDesc')}</p>
          {#if composerInstalled}
            <div class="flex items-center gap-2">
              <span class="text-[10px] px-1.5 py-0.5 bg-emerald-500/10 text-emerald-500 rounded border border-emerald-500/20 font-bold uppercase">
                {$t('runtimes.labelInstalled')}
              </span>
              {#if composerVersion}
                <span class="text-xs font-mono text-[var(--color-text-secondary)]">v{composerVersion}</span>
              {/if}
            </div>
          {:else}
            <button
              class="btn-primary text-xs"
              on:click={installComposer}
              disabled={installingComposer}
            >
              {#if installingComposer}
                <div class="w-3 h-3 border-2 border-white border-t-transparent rounded-full animate-spin inline-block mr-1"></div>
              {/if}
              {$t('runtimes.installComposer')}
            </button>
          {/if}
        </div>

        <!-- PHP-CGI Card -->
        <div class="card p-5">
          <h3 class="text-base font-bold mb-1">{$t('runtimes.phpCgi')}</h3>
          <p class="text-xs text-[var(--color-text-secondary)] mb-3">{$t('runtimes.phpCgiDesc')}</p>
          <div class="flex items-center gap-2">
            <input
              type="number"
              bind:value={phpCgiPort}
              class="w-20 px-2 py-1 text-xs font-mono rounded-lg border border-[var(--color-border)] bg-[var(--color-bg)]"
              min="1" max="65535"
              disabled={phpCgiRunning}
            />
            <button
              class="text-xs px-3 py-1.5 rounded-lg font-medium {phpCgiRunning ? 'bg-red-500/10 text-red-500 border border-red-500/20 hover:bg-red-500/20' : 'bg-emerald-500/10 text-emerald-500 border border-emerald-500/20 hover:bg-emerald-500/20'}"
              on:click={togglePhpCgi}
            >
              {phpCgiRunning ? $t('runtimes.stopPhpCgi') : $t('runtimes.startPhpCgi')}
            </button>
          </div>
          {#if phpCgiRunning}
            <p class="text-xs text-emerald-500 mt-2">{$t('runtimes.phpCgiRunning').replace('{0}', String(phpCgiPort))}</p>
          {/if}
        </div>
      </div>
    {/if}

    <!-- Node.js Package Managers (only when Node tab is active) -->
    {#if activeTab === 'node' && installedVersions.length > 0}
      <div class="card p-5">
        <h3 class="text-base font-bold mb-1">{$t('runtimes.packageManagers')}</h3>
        <p class="text-xs text-[var(--color-text-secondary)] mb-3">{$t('runtimes.packageManagersDesc')}</p>
        <div class="space-y-2">
          <!-- npm (built-in) -->
          <div class="flex items-center justify-between px-3 py-2 rounded-lg border border-[var(--color-border)] bg-[var(--color-bg)]">
            <div class="flex items-center gap-2">
              <span class="text-sm font-bold">npm</span>
              <span class="text-[10px] px-1.5 py-0.5 bg-emerald-500/10 text-emerald-500 rounded border border-emerald-500/20 font-bold uppercase">{$t('runtimes.builtIn')}</span>
            </div>
          </div>
          <!-- Bun -->
          <div class="flex items-center justify-between px-3 py-2 rounded-lg border border-[var(--color-border)] bg-[var(--color-bg)]">
            <div class="flex items-center gap-2">
              <span class="text-sm font-bold">Bun</span>
              {#if bunInstalled}
                <span class="text-[10px] px-1.5 py-0.5 bg-emerald-500/10 text-emerald-500 rounded border border-emerald-500/20 font-bold uppercase">{$t('runtimes.labelInstalled')}</span>
                {#if bunVersion}
                  <span class="text-xs font-mono text-[var(--color-text-secondary)]">v{bunVersion}</span>
                {/if}
              {/if}
            </div>
            <div class="flex items-center gap-2">
              {#if bunInstalled}
                <button
                  class="text-xs px-2.5 py-1 rounded-lg text-red-500 hover:bg-red-500/10 border border-red-500/20"
                  on:click={uninstallBun}
                  disabled={uninstallingBun}
                >
                  {#if uninstallingBun}
                    <div class="w-3 h-3 border-2 border-red-500 border-t-transparent rounded-full animate-spin inline-block mr-1"></div>
                  {/if}
                  {$t('runtimes.uninstallBun')}
                </button>
              {:else}
                <button
                  class="text-xs px-2.5 py-1 rounded-lg bg-blue-600 text-white hover:bg-blue-700"
                  on:click={installBun}
                  disabled={installingBun}
                >
                  {#if installingBun}
                    <div class="w-3 h-3 border-2 border-white border-t-transparent rounded-full animate-spin inline-block mr-1"></div>
                  {/if}
                  {$t('runtimes.installBun')}
                </button>
              {/if}
            </div>
          </div>
        </div>
      </div>
    {/if}

    <!-- Available Versions (Remote) -->
    <div class="card p-5">
      <div class="flex items-center justify-between mb-4">
        <h3 class="text-base font-bold flex items-center gap-2">
          {$t('runtimes.available')}
        </h3>
        <div class="flex items-center gap-4">
          {#if loadingRemote}
            <span class="text-xs text-[var(--color-text-secondary)] animate-pulse">{$t('common.loading')}</span>
          {/if}
          <button class="text-xs text-primary-500 hover:underline flex items-center gap-1" on:click={loadRemote} disabled={loadingRemote}>
            <svg class="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M23 4v6h-6M1 20v-6h6M3.51 9a9 9 0 0114.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0020.49 15"/></svg>
            {$t('common.refresh')}
          </button>
        </div>
      </div>

      {#if remoteVersions.length > 0}
        <div class="grid grid-cols-2 md:grid-cols-3 lg:grid-cols-4 gap-2 max-h-[400px] overflow-y-auto pr-2">
          {#each remoteVersions as ver}
            <div class="flex items-center justify-between p-3 rounded-lg border border-[var(--color-border)] hover:border-primary-500/30 transition-all bg-[var(--color-bg)]/50">
              <div class="flex flex-col gap-1 min-w-0">
                <span class="font-mono text-xs font-bold truncate">{ver.number}</span>
                <div class="flex gap-1">
                  {#if ver.stable}
                    <span class="text-[9px] px-1 bg-emerald-500/10 text-emerald-500 rounded border border-emerald-500/20 uppercase font-bold">{$t('runtimes.labelStable')}</span>
                  {/if}
                  {#if ver.installed}
                    <span class="text-[9px] px-1 bg-blue-500/10 text-blue-500 rounded border border-blue-500/20 uppercase font-bold">{$t('runtimes.labelInstalled')}</span>
                  {/if}
                </div>
              </div>
              {#if !ver.installed}
                <button
                  class="btn-icon bg-blue-600 hover:bg-blue-700 text-white shadow-sm h-8 w-8"
                  on:click={() => install(ver.number)}
                  disabled={installing !== ''}
                  title={$t('runtimes.install')}
                >
                  {#if installing === ver.number}
                    <div class="w-3 h-3 border-2 border-white border-t-transparent rounded-full animate-spin"></div>
                  {:else}
                    <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/></svg>
                  {/if}
                </button>
              {/if}
            </div>
          {/each}
        </div>
      {:else if loadingRemote}
        <div class="text-center py-12">
          <div class="w-8 h-8 border-3 border-primary-500 border-t-transparent rounded-full animate-spin mx-auto mb-4"></div>
          <p class="text-sm text-[var(--color-text-secondary)]">{$t('runtimes.loadingVersions').replace('{0}', activeTab)}</p>
        </div>
      {:else}
        <div class="text-center py-12 text-[var(--color-text-secondary)]">
          <p class="text-sm">{$t('runtimes.emptyAvailable')}</p>
        </div>
      {/if}
    </div>
  </div>
</div>
