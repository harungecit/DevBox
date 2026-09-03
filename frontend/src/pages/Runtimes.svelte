<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { t } from '../lib/i18n/index';
  import StatusBadge from '../lib/components/StatusBadge.svelte';
  import ProgressBar from '../lib/components/ProgressBar.svelte';
  import ConfirmDialog from '../lib/components/ConfirmDialog.svelte';
  import { runtimeLogo } from '../lib/logos';
  import {
    GetRemoteVersionsInfo,
    GetInstalledVersions,
    InstallRuntime,
    UpdateRuntime,
    UninstallRuntime,
    SetGlobalRuntime,
    GetPHPExtensions,
    TogglePHPExtension,
    GetPeclExtensions,
    InstallPeclExtension,
    UninstallPeclExtension,
    GetPHPCGIInstances,
    RestartPHPCGI,
    StopPHPCGI,
    GetPHPIniSettings,
    SetPHPIniSetting,
    GetPHPIniPath,
    OpenFileInEditor,
  } from '../../wailsjs/go/main/App';
  import ImportCenter from '../lib/components/ImportCenter.svelte';
  import RuntimeCatalog from '../lib/components/RuntimeCatalog.svelte';
  import PluginRuntimeHeader from '../lib/components/PluginRuntimeHeader.svelte';
  import { runtimeCatalog, loadRuntimeCatalog } from '../lib/stores/runtimes';
  import type { RuntimeMeta } from '../lib/stores/runtimes';
  import { EventsOn } from '../../wailsjs/runtime/runtime';
  import { runtimeInstalls, startRuntimeInstall, clearRuntimeInstall } from '../lib/stores/installs';

  interface VersionInfo {
    number: string;
    stable: boolean;
    current: boolean;
    installed: boolean;
    updateFor?: string;
  }

  interface PHPExtension {
    name: string;
    enabled: boolean;
    zend?: boolean;
    source?: string;
  }

  interface PeclExtension {
    name: string;
    description: string;
    installed: boolean;
    version: string;
    zend: boolean;
  }

  let peclExtensions: PeclExtension[] = [];
  let peclBusy: Record<string, { percent: number; message: string }> = {};

  async function loadPecl(version: string) {
    try {
      peclExtensions = (await GetPeclExtensions(version)) || [];
    } catch {
      peclExtensions = [];
    }
  }

  async function installPecl(name: string) {
    const active = installedVersions.find(v => v.current);
    if (!active) return;
    peclBusy = { ...peclBusy, [name]: { percent: 0, message: '' } };
    errorMessage = '';
    try {
      await InstallPeclExtension(active.number, name);
    } catch (e) {
      errorMessage = String(e);
      const { [name]: _r, ...rest } = peclBusy; peclBusy = rest;
    }
  }

  async function removePecl(name: string) {
    const active = installedVersions.find(v => v.current);
    if (!active) return;
    try {
      await UninstallPeclExtension(active.number, name);
      await loadExtensions(active.number);
      await loadPecl(active.number);
    } catch (e) {
      errorMessage = String(e);
    }
  }

  interface PHPCGIInstance {
    version: string;
    port: number;
    pid: number;
  }

  // Import Center modal (external installations found on this machine).
  let showImportCenter: boolean = false;
  // Runtime catalog modal (add a language/tool from the vfox plugin registry).
  let showRuntimeCatalog: boolean = false;

  let activeTab: string = '';
  let installedVersions: VersionInfo[] = [];
  let remoteVersions: VersionInfo[] = [];
  let loadingRemote: boolean = false;
  let loadingInstalled: boolean = false;
  let errorMessage: string = '';
  let fetchedAt: string = '';

  // Install state for the active tab — derived from the global store so it
  // survives leaving and returning to this page.
  $: currentInstall = $runtimeInstalls[activeTab];
  $: installing = currentInstall?.version ?? '';

  // installed version → newest version of its release line (in-place update)
  $: updates = (() => {
    const map: Record<string, string> = {};
    for (const v of remoteVersions) {
      if (!v.updateFor) continue;
      const cur = map[v.updateFor];
      if (!cur || compareVersions(v.number, cur) > 0) map[v.updateFor] = v.number;
    }
    return map;
  })();
  $: updateCount = Object.keys(updates).length;

  function compareVersions(a: string, b: string): number {
    const pa = a.split('.').map(Number), pb = b.split('.').map(Number);
    for (let i = 0; i < Math.max(pa.length, pb.length); i++) {
      const d = (pa[i] || 0) - (pb[i] || 0);
      if (d !== 0) return d;
    }
    return 0;
  }

  // PHP-specific state
  let phpExtensions: PHPExtension[] = [];
  let loadingExtensions: boolean = false;
  let phpCgiInstances: PHPCGIInstance[] = [];
  let phpCgiBusy: boolean = false;
  let extensionFilter: string = '';

  // PHP Settings state
  let phpIniSettings: Record<string, string> = {};
  let phpIniEdited: Record<string, string> = {};
  let loadingIniSettings: boolean = false;
  let savingIniSettings: boolean = false;
  let iniSaveMessage: string = '';

  const iniDirectives = [
    { key: 'max_execution_time', type: 'text', suffix: 'seconds' },
    { key: 'memory_limit', type: 'text', suffix: '' },
    { key: 'upload_max_filesize', type: 'text', suffix: '' },
    { key: 'post_max_size', type: 'text', suffix: '' },
    { key: 'max_file_uploads', type: 'text', suffix: '' },
    { key: 'display_errors', type: 'select', options: ['On', 'Off'] },
    { key: 'error_reporting', type: 'select', options: ['E_ALL', 'E_ALL & ~E_NOTICE', 'E_ALL & ~E_DEPRECATED & ~E_STRICT', 'E_ERROR | E_WARNING | E_PARSE'] },
    { key: 'date.timezone', type: 'text', suffix: '' },
  ];

  // Tabs come from the runtime catalog: built-ins first, then plugin runtimes.
  interface Tab { id: string; label: string; plugin: boolean; count: number }
  $: tabs = $runtimeCatalog.map((m) => ({ id: m.name, label: m.displayName, plugin: m.plugin, count: m.installed })) as Tab[];
  $: builtinTabs = tabs.filter((tab) => !tab.plugin);
  $: pluginTabs = tabs.filter((tab) => tab.plugin);
  $: activeMeta = $runtimeCatalog.find((m) => m.name === activeTab) as RuntimeMeta | undefined;
  $: activeLabel = activeMeta?.displayName ?? activeTab;
  // Select a tab once the catalog arrives (preferring one with installed
  // versions) and fall back when the active runtime disappears (plugin removed).
  $: if (tabs.length > 0 && !tabs.some((tab) => tab.id === activeTab)) {
    const first = tabs.find((tab) => tab.count > 0) ?? tabs[0];
    switchTab(first.id);
  }

  // Version list filtering — plugin lists (Java, .NET…) can run into the hundreds.
  let versionFilter: string = '';
  let stableOnlyByTab: Record<string, boolean> = {};
  let showAllVersions: boolean = false;
  const VERSION_ROW_CAP = 200;
  $: stableOnly = stableOnlyByTab[activeTab] ?? (activeMeta?.plugin === true || remoteVersions.length > 60);
  $: visibleRemote = remoteVersions.filter((v) =>
    (!stableOnly || v.stable || v.installed) &&
    (!versionFilter || v.number.toLowerCase().includes(versionFilter.toLowerCase()))
  );
  $: cappedRemote = showAllVersions ? visibleRemote : visibleRemote.slice(0, VERSION_ROW_CAP);
  function toggleStableOnly() {
    stableOnlyByTab = { ...stableOnlyByTab, [activeTab]: !stableOnly };
  }

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

  async function loadRemote(force: boolean = false) {
    loadingRemote = true;
    errorMessage = '';
    try {
      const result = await GetRemoteVersionsInfo(activeTab, force);
      remoteVersions = result?.versions || [];
      fetchedAt = result?.fetchedAt ? new Date(result.fetchedAt).toLocaleString() : '';
    } catch (e) {
      console.error('Failed to load remote:', e);
      errorMessage = String(e);
      remoteVersions = [];
    }
    loadingRemote = false;
  }

  async function switchTab(tab: string) {
    activeTab = tab;
    errorMessage = '';
    versionFilter = '';
    showAllVersions = false;
    await Promise.all([loadInstalled(), loadRemote()]);
    if (tab === 'php') {
      await loadPHPExtras();
    }
  }

  async function loadPHPExtras() {
    const active = installedVersions.find(v => v.current);
    if (active) {
      await loadExtensions(active.number);
      await loadPHPIniSettings();
      await loadPecl(active.number);
    }
    await loadPhpCgi();
  }

  async function loadPhpCgi() {
    try {
      phpCgiInstances = (await GetPHPCGIInstances()) || [];
    } catch {
      phpCgiInstances = [];
    }
  }

  async function loadPHPIniSettings() {
    const active = installedVersions.find(v => v.current);
    if (!active) return;
    loadingIniSettings = true;
    try {
      const settings = await GetPHPIniSettings(active.number);
      phpIniSettings = settings || {};
      phpIniEdited = { ...phpIniSettings };
    } catch (e) {
      console.error('Failed to load PHP ini settings:', e);
    }
    loadingIniSettings = false;
  }

  async function savePHPIniSettings() {
    const active = installedVersions.find(v => v.current);
    if (!active) return;
    savingIniSettings = true;
    iniSaveMessage = '';
    try {
      for (const dir of iniDirectives) {
        if (phpIniEdited[dir.key] !== phpIniSettings[dir.key]) {
          await SetPHPIniSetting(active.number, dir.key, phpIniEdited[dir.key]);
        }
      }
      phpIniSettings = { ...phpIniEdited };
      iniSaveMessage = phpCgiInstances.length > 0 ? 'restart' : 'saved';
      setTimeout(() => { iniSaveMessage = ''; }, 4000);
    } catch (e) {
      errorMessage = String(e);
    }
    savingIniSettings = false;
  }

  async function openPhpIni() {
    const active = installedVersions.find(v => v.current);
    if (!active) return;
    try {
      const path = await GetPHPIniPath(active.number);
      await OpenFileInEditor(path);
    } catch (e) {
      errorMessage = String(e);
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

  async function restartPhpCgi() {
    phpCgiBusy = true;
    try {
      await RestartPHPCGI();
      await loadPhpCgi();
    } catch (e) {
      errorMessage = String(e);
    }
    phpCgiBusy = false;
  }

  async function stopPhpCgi() {
    phpCgiBusy = true;
    try {
      await StopPHPCGI();
      await loadPhpCgi();
    } catch (e) {
      errorMessage = String(e);
    }
    phpCgiBusy = false;
  }

  async function install(version: string) {
    startRuntimeInstall(activeTab, version);
    errorMessage = '';
    try {
      await InstallRuntime(activeTab, version);
    } catch (e) {
      errorMessage = String(e);
      clearRuntimeInstall(activeTab);
    }
  }

  async function update(from: string, to: string) {
    startRuntimeInstall(activeTab, to);
    errorMessage = '';
    try {
      await UpdateRuntime(activeTab, from, to);
    } catch (e) {
      errorMessage = String(e);
      clearRuntimeInstall(activeTab);
    }
  }

  function dismissInstallError() {
    clearRuntimeInstall(activeTab);
  }

  // Uninstall confirmation + busy state.
  let pendingUninstall: string | null = null;
  let uninstallingVersion: string | null = null;

  function requestUninstall(version: string) {
    pendingUninstall = version;
  }

  function cancelUninstall() {
    if (uninstallingVersion) return;
    pendingUninstall = null;
  }

  async function confirmUninstall() {
    if (!pendingUninstall) return;
    const version = pendingUninstall;
    uninstallingVersion = version;
    try {
      await UninstallRuntime(activeTab, version);
      await Promise.all([loadInstalled(), loadRemote(), loadRuntimeCatalog()]);
    } catch (e) {
      errorMessage = String(e);
    }
    uninstallingVersion = null;
    pendingUninstall = null;
  }

  async function setGlobal(version: string) {
    try {
      await SetGlobalRuntime(activeTab, version);
      await Promise.all([loadInstalled(), loadRuntimeCatalog()]);
      if (activeTab === 'php') await loadPHPExtras();
    } catch (e) {
      errorMessage = String(e);
    }
  }

  let eventUnsubs: Array<() => void> = [];

  onMount(() => {
    eventUnsubs.push(EventsOn('runtime:installed', (data: any) => {
      if (data?.name === activeTab) {
        loadInstalled();
        loadRemote();
        if (activeTab === 'php') loadPHPExtras();
      }
    }));
    eventUnsubs.push(EventsOn('versions:refreshed', () => {
      loadRemote();
    }));
    eventUnsubs.push(EventsOn('runtimes:changed', () => {
      loadRuntimeCatalog();
    }));
    eventUnsubs.push(EventsOn('phpext:progress', (d: any) => {
      if (!d?.name) return;
      peclBusy = { ...peclBusy, [d.name]: { percent: d.percent ?? 0, message: d.message ?? '' } };
    }));
    eventUnsubs.push(EventsOn('phpext:installed', (d: any) => {
      const { [d?.name]: _r, ...rest } = peclBusy; peclBusy = rest;
      const active = installedVersions.find(v => v.current);
      if (active) { loadExtensions(active.number); loadPecl(active.number); }
    }));
    eventUnsubs.push(EventsOn('phpext:error', (d: any) => {
      const { [d?.name]: _r, ...rest } = peclBusy; peclBusy = rest;
      errorMessage = `${d?.name}: ${d?.error || 'install failed'}`;
    }));
    // The tab-selection reactive block loads versions once the catalog is in.
    loadRuntimeCatalog();
  });

  onDestroy(() => {
    eventUnsubs.forEach(fn => fn());
    eventUnsubs = [];
  });
</script>

<div class="space-y-6">
  <div class="flex items-start justify-between gap-4">
    <div>
      <h2 class="text-2xl font-bold">{$t('runtimes.title')}</h2>
      <p class="text-[var(--color-text-secondary)] mt-1">{$t('runtimes.subtitle')}</p>
    </div>
    <div class="flex items-center gap-2 shrink-0 mt-1">
      <button
        class="text-xs px-3 py-1.5 rounded-lg font-medium border border-[var(--color-border)] text-[var(--color-text-secondary)] hover:text-primary-500 hover:border-primary-500/40 transition-colors flex items-center gap-1.5"
        on:click={() => showImportCenter = true}
        title={$t('discovery.centerTitle')}
      >
        <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/></svg>
        {$t('discovery.centerButton')}
      </button>
      <button
        class="text-xs px-3 py-1.5 rounded-lg font-medium bg-primary-600 hover:bg-primary-700 text-white shadow-sm transition-colors flex items-center gap-1.5"
        on:click={() => showRuntimeCatalog = true}
        title={$t('plugins.subtitle')}
      >
        <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/></svg>
        {$t('runtimes.addRuntime')}
      </button>
    </div>
  </div>

  <!-- Runtime Tabs -->
  <div class="flex gap-1 border-b border-[var(--color-border)] overflow-x-auto">
    {#each builtinTabs as tab (tab.id)}
      <button
        class="px-4 py-2.5 text-sm font-medium transition-colors border-b-2 -mb-px flex items-center gap-2 shrink-0"
        class:border-primary-500={activeTab === tab.id}
        class:text-primary-500={activeTab === tab.id}
        class:border-transparent={activeTab !== tab.id}
        class:text-[var(--color-text-secondary)]={activeTab !== tab.id}
        on:click={() => switchTab(tab.id)}
        disabled={installing !== ''}
      >
        <div class="w-5 h-5 rounded overflow-hidden">{@html runtimeLogo(tab.id, tab.label)}</div>
        {tab.label}
        {#if tab.count > 0}
          <span class="min-w-[18px] h-[18px] px-1 rounded-full text-[10px] font-bold flex items-center justify-center {activeTab === tab.id ? 'bg-primary-500 text-white' : 'bg-slate-200 dark:bg-slate-700 text-[var(--color-text-secondary)]'}">
            {tab.count}
          </span>
        {/if}
      </button>
    {/each}
    {#if pluginTabs.length > 0}
      <div class="w-px h-5 bg-[var(--color-border)] self-center mx-1 shrink-0"></div>
      {#each pluginTabs as tab (tab.id)}
        <button
          class="px-4 py-2.5 text-sm font-medium transition-colors border-b-2 -mb-px flex items-center gap-2 shrink-0"
          class:border-primary-500={activeTab === tab.id}
          class:text-primary-500={activeTab === tab.id}
          class:border-transparent={activeTab !== tab.id}
          class:text-[var(--color-text-secondary)]={activeTab !== tab.id}
          on:click={() => switchTab(tab.id)}
          disabled={installing !== ''}
          title={$t('runtimes.pluginTabHint')}
        >
          <div class="w-5 h-5 rounded overflow-hidden">{@html runtimeLogo(tab.id, tab.label)}</div>
          {tab.label}
          {#if tab.count > 0}
            <span class="min-w-[18px] h-[18px] px-1 rounded-full text-[10px] font-bold flex items-center justify-center {activeTab === tab.id ? 'bg-primary-500 text-white' : 'bg-slate-200 dark:bg-slate-700 text-[var(--color-text-secondary)]'}">
              {tab.count}
            </span>
          {/if}
        </button>
      {/each}
    {/if}
    <button
      class="px-3 py-2.5 text-xs font-medium transition-colors border-b-2 border-transparent -mb-px flex items-center gap-1.5 shrink-0 text-[var(--color-text-secondary)] hover:text-primary-500"
      on:click={() => showRuntimeCatalog = true}
      disabled={installing !== ''}
      title={$t('runtimes.addRuntime')}
    >
      <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/></svg>
      {$t('runtimes.addRuntime')}
    </button>
  </div>

  <!-- Error message -->
  {#if errorMessage}
    <div class="p-3 rounded-lg bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 text-red-700 dark:text-red-400 text-sm">
      {errorMessage}
      <button class="ml-2 underline" on:click={() => errorMessage = ''}>{$t('common.dismiss')}</button>
    </div>
  {/if}

  <!-- Progress bar / install error when an install is in flight or just failed -->
  {#if currentInstall}
    {#if currentInstall.error}
      <div class="p-3 rounded-lg bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 text-red-700 dark:text-red-400 text-sm">
        <span class="font-medium">{activeLabel} {currentInstall.version}:</span>
        {currentInstall.error}
        <button class="ml-2 underline" on:click={dismissInstallError}>{$t('common.dismiss')}</button>
      </div>
    {:else}
      <div class="card">
        <div class="flex items-center gap-3 mb-2">
          <div class="w-2 h-2 rounded-full bg-primary-500 animate-pulse"></div>
          <span class="text-sm font-medium">{currentInstall.importing ? $t('discovery.importing') : $t('runtimes.downloading')} {activeLabel} {currentInstall.version}</span>
        </div>
        <ProgressBar percent={currentInstall.percent} message={currentInstall.message} />
      </div>
    {/if}
  {/if}

  <div class="space-y-6">
    {#if activeMeta && activeMeta.plugin}
      <PluginRuntimeHeader meta={activeMeta} busy={installing !== '' || uninstallingVersion !== null} on:removed={() => loadRuntimeCatalog()} />
    {/if}

    <!-- Installed Versions -->
    <div class="card p-5">
      <div class="flex items-center justify-between mb-4">
        <h3 class="text-base font-bold flex items-center gap-2">
          {$t('runtimes.installed')}
          <span class="badge badge-info">{installedVersions.length}</span>
          {#if updateCount > 0}
            <span class="text-[10px] px-1.5 py-0.5 rounded-full bg-amber-500/10 text-amber-600 dark:text-amber-400 border border-amber-500/20 font-bold uppercase">{$t('runtimes.updatesAvailable', updateCount)}</span>
          {/if}
        </h3>
        {#if loadingInstalled}
          <span class="text-xs text-[var(--color-text-secondary)] animate-pulse">{$t('common.loading')}</span>
        {/if}
      </div>

      {#if installedVersions.length > 0}
        <div class="space-y-2">
          {#each installedVersions as ver}
            {@const target = updates[ver.number]}
            <div class="flex items-center justify-between p-4 rounded-xl border border-[var(--color-border)] hover:bg-[var(--color-bg)] transition-colors">
              <div class="flex items-center gap-4">
                <div class="w-8 h-8 rounded-lg overflow-hidden shadow-sm">
                  {@html runtimeLogo(activeTab, activeLabel)}
                </div>
                <span class="font-mono text-sm font-semibold">{ver.number}</span>
                {#if ver.current}
                  <span title={$t('runtimes.activeHint')}>
                    <StatusBadge status="active" label={$t('runtimes.active')} />
                  </span>
                {/if}
                {#if target}
                  <span class="text-[10px] px-1.5 py-0.5 rounded bg-amber-500/10 text-amber-600 dark:text-amber-400 border border-amber-500/20 font-bold font-mono">→ {target}</span>
                {/if}
              </div>
              <div class="flex gap-2">
                {#if target}
                  <button
                    class="btn-icon bg-amber-500 hover:bg-amber-600 text-white shadow-sm"
                    on:click={() => update(ver.number, target)}
                    disabled={installing !== ''}
                    title="{$t('runtimes.updateTo', target)} — {$t('runtimes.updateHint')}"
                  >
                    {#if installing === target}
                      <div class="w-3 h-3 border-2 border-white border-t-transparent rounded-full animate-spin"></div>
                    {:else}
                      <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="17 8 12 3 7 8"/><line x1="12" y1="3" x2="12" y2="15"/></svg>
                    {/if}
                  </button>
                {/if}
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
                  on:click={() => requestUninstall(ver.number)}
                  disabled={installing !== '' || uninstallingVersion !== null}
                  title={$t('runtimes.uninstall')}
                >
                  {#if uninstallingVersion === ver.number}
                    <div class="w-4 h-4 border-2 border-red-500 border-t-transparent rounded-full animate-spin"></div>
                  {:else}
                    <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M3 6h18M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/></svg>
                  {/if}
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
                <span class="font-mono truncate flex items-center gap-1">
                  {ext.name}
                  {#if ext.source === 'pecl'}<span class="text-[8px] px-1 rounded bg-purple-500/10 text-purple-500 uppercase font-bold">{$t('runtimes.labelPecl')}</span>{/if}
                  {#if ext.zend}<span class="text-[8px] px-1 rounded bg-slate-500/10 text-slate-500 uppercase font-bold">{$t('runtimes.peclZend')}</span>{/if}
                </span>
                <span class="text-[10px] font-bold ml-1 {ext.enabled ? 'text-emerald-500' : 'text-slate-400'}">
                  {ext.enabled ? $t('runtimes.enabled') : $t('runtimes.disabled')}
                </span>
              </button>
            {/each}
          </div>
        {:else}
          <p class="text-xs text-[var(--color-text-secondary)]">No extensions found in php.ini</p>
        {/if}

        <!-- PECL catalog -->
        <div class="mt-5 pt-4 border-t border-[var(--color-border)]">
          <h4 class="text-sm font-bold">{$t('runtimes.peclTitle')}</h4>
          <p class="text-xs text-[var(--color-text-secondary)] mb-3">{$t('runtimes.peclDesc')}</p>
          <div class="grid grid-cols-1 md:grid-cols-2 gap-1.5">
            {#each peclExtensions as pe}
              {@const busy = peclBusy[pe.name]}
              <div class="flex items-center justify-between gap-3 px-2.5 py-1.5 rounded-lg border text-xs {pe.installed ? 'border-purple-500/30 bg-purple-500/5' : 'border-[var(--color-border)] bg-[var(--color-bg)]'}">
                <div class="min-w-0">
                  <div class="flex items-center gap-1.5">
                    <span class="font-mono font-bold">{pe.name}</span>
                    {#if pe.installed && pe.version}<span class="font-mono text-[10px] text-[var(--color-text-secondary)]">{pe.version}</span>{/if}
                    {#if pe.zend}<span class="text-[8px] px-1 rounded bg-slate-500/10 text-slate-500 uppercase font-bold">{$t('runtimes.peclZend')}</span>{/if}
                  </div>
                  <p class="text-[10px] text-[var(--color-text-secondary)] truncate">{busy ? (busy.message || $t('runtimes.peclInstalling')) : pe.description}</p>
                </div>
                {#if busy}
                  <div class="w-4 h-4 border-2 border-purple-500 border-t-transparent rounded-full animate-spin flex-shrink-0"></div>
                {:else if pe.installed}
                  <button class="text-[10px] px-2 py-1 rounded-lg text-red-500 border border-red-500/20 hover:bg-red-500/10 flex-shrink-0" on:click={() => removePecl(pe.name)}>{$t('runtimes.peclRemove')}</button>
                {:else}
                  <button class="text-[10px] px-2 py-1 rounded-lg bg-purple-600 text-white hover:bg-purple-700 flex-shrink-0 disabled:opacity-50" on:click={() => installPecl(pe.name)} disabled={installing !== ''}>{$t('runtimes.peclInstall')}</button>
                {/if}
              </div>
            {/each}
          </div>
        </div>
      </div>

      <!-- PHP Settings Card -->
      <div class="card p-5">
        <div class="flex items-center justify-between mb-1">
          <h3 class="text-base font-bold">{$t('runtimes.phpSettings')}</h3>
          <button
            class="text-xs px-3 py-1.5 rounded-lg font-medium border border-[var(--color-border)] hover:bg-[var(--color-bg)] transition-colors"
            on:click={openPhpIni}
          >
            {$t('runtimes.phpOpenIni')}
          </button>
        </div>
        <p class="text-xs text-[var(--color-text-secondary)] mb-4">{$t('runtimes.phpSettingsDesc')}</p>

        {#if loadingIniSettings}
          <span class="text-xs text-[var(--color-text-secondary)] animate-pulse">{$t('common.loading')}</span>
        {:else}
          <div class="space-y-2.5">
            {#each iniDirectives as dir}
              <div class="flex items-center gap-3">
                <span class="font-mono text-xs text-[var(--color-text-secondary)] w-44 shrink-0">{dir.key}</span>
                {#if dir.type === 'select'}
                  <select
                    class="flex-1 px-2 py-1.5 text-xs font-mono rounded-lg border border-[var(--color-border)] bg-[var(--color-bg)]"
                    bind:value={phpIniEdited[dir.key]}
                  >
                    {#each dir.options as opt}
                      <option value={opt}>{opt}</option>
                    {/each}
                  </select>
                {:else}
                  <input
                    type="text"
                    class="flex-1 px-2 py-1.5 text-xs font-mono rounded-lg border border-[var(--color-border)] bg-[var(--color-bg)]"
                    bind:value={phpIniEdited[dir.key]}
                  />
                {/if}
                {#if dir.suffix}
                  <span class="text-xs text-[var(--color-text-secondary)] shrink-0">{$t('runtimes.' + dir.suffix) || dir.suffix}</span>
                {/if}
              </div>
            {/each}
          </div>

          <div class="flex items-center gap-3 mt-4">
            <button
              class="btn-primary text-xs"
              on:click={savePHPIniSettings}
              disabled={savingIniSettings}
            >
              {#if savingIniSettings}
                <div class="w-3 h-3 border-2 border-white border-t-transparent rounded-full animate-spin inline-block mr-1"></div>
              {/if}
              {$t('runtimes.phpSave')}
            </button>
            {#if iniSaveMessage === 'saved'}
              <span class="text-xs text-emerald-500">{$t('runtimes.phpSettingsSaved')}</span>
            {:else if iniSaveMessage === 'restart'}
              <span class="text-xs text-amber-500">{$t('runtimes.phpSettingsSaved')} — {$t('runtimes.phpRestartCgiHint')}</span>
              <button class="text-xs text-primary-500 hover:underline" on:click={restartPhpCgi} disabled={phpCgiBusy}>{$t('runtimes.restartPhpCgi')}</button>
            {/if}
          </div>
        {/if}
      </div>

      <!-- PHP-CGI Card -->
      <div class="card p-5">
        <div class="flex items-center justify-between mb-1">
          <h3 class="text-base font-bold">{$t('runtimes.phpCgi')}</h3>
          <div class="flex items-center gap-2">
            <button
              class="text-xs px-3 py-1.5 rounded-lg font-medium bg-emerald-500/10 text-emerald-500 border border-emerald-500/20 hover:bg-emerald-500/20 disabled:opacity-50"
              on:click={restartPhpCgi}
              disabled={phpCgiBusy}
            >
              {#if phpCgiBusy}<div class="w-3 h-3 border-2 border-emerald-500 border-t-transparent rounded-full animate-spin inline-block mr-1"></div>{/if}
              {$t('runtimes.restartPhpCgi')}
            </button>
            {#if phpCgiInstances.length > 0}
              <button
                class="text-xs px-3 py-1.5 rounded-lg font-medium bg-red-500/10 text-red-500 border border-red-500/20 hover:bg-red-500/20 disabled:opacity-50"
                on:click={stopPhpCgi}
                disabled={phpCgiBusy}
              >
                {$t('runtimes.stopAllPhpCgi')}
              </button>
            {/if}
          </div>
        </div>
        <p class="text-xs text-[var(--color-text-secondary)] mb-3">{$t('runtimes.phpCgiDesc')}</p>
        {#if phpCgiInstances.length > 0}
          <div class="text-[10px] font-bold uppercase tracking-wider text-[var(--color-text-secondary)] mb-1.5">{$t('runtimes.phpCgiInstances')}</div>
          <div class="flex flex-wrap gap-2">
            {#each phpCgiInstances as inst}
              <div class="flex items-center gap-2 px-2.5 py-1.5 rounded-lg border border-emerald-500/30 bg-emerald-500/5">
                <span class="w-1.5 h-1.5 rounded-full bg-emerald-500"></span>
                <span class="font-mono text-xs font-bold">PHP {inst.version}</span>
                <span class="font-mono text-[10px] text-[var(--color-text-secondary)]">127.0.0.1:{inst.port}</span>
                <span class="font-mono text-[10px] text-slate-400">pid {inst.pid}</span>
              </div>
            {/each}
          </div>
        {:else}
          <p class="text-xs text-[var(--color-text-secondary)] italic">{$t('runtimes.phpCgiNone')}</p>
        {/if}
      </div>
    {/if}

    <!-- Available Versions (Remote) -->
    <div class="card p-5">
      <div class="flex items-center justify-between mb-4">
        <h3 class="text-base font-bold flex items-center gap-2">
          {$t('runtimes.available')}
          {#if remoteVersions.length > 0}
            <span class="text-[10px] font-normal text-[var(--color-text-secondary)]">{$t('runtimes.showingVersions', String(visibleRemote.length), String(remoteVersions.length))}</span>
          {/if}
        </h3>
        <div class="flex items-center gap-3">
          {#if remoteVersions.length > 0}
            <input
              type="text"
              bind:value={versionFilter}
              placeholder={$t('runtimes.filterVersions')}
              class="px-2 py-1 text-xs rounded-lg border border-[var(--color-border)] bg-[var(--color-bg)] w-32 font-mono"
            />
            <button
              class="text-[10px] px-2 py-1 rounded-lg border font-bold uppercase transition-colors {stableOnly ? 'border-emerald-500/30 bg-emerald-500/10 text-emerald-500' : 'border-[var(--color-border)] text-[var(--color-text-secondary)]'}"
              on:click={toggleStableOnly}
              title={$t('runtimes.stableOnly')}
            >{$t('runtimes.stableOnly')}</button>
          {/if}
          {#if loadingRemote}
            <span class="text-xs text-[var(--color-text-secondary)] animate-pulse">{$t('common.loading')}</span>
          {:else if fetchedAt}
            <span class="text-[10px] text-[var(--color-text-secondary)]">{$t('runtimes.cachedAt', fetchedAt)}</span>
          {/if}
          <button class="text-xs text-primary-500 hover:underline flex items-center gap-1" on:click={() => loadRemote(true)} disabled={loadingRemote}>
            <svg class="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M23 4v6h-6M1 20v-6h6M3.51 9a9 9 0 0114.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0020.49 15"/></svg>
            {$t('common.refresh')}
          </button>
        </div>
      </div>

      {#if remoteVersions.length > 0 && visibleRemote.length === 0}
        <div class="text-center py-8 text-[var(--color-text-secondary)]">
          <p class="text-sm">{$t('runtimes.filterNoMatch')}</p>
        </div>
      {:else if remoteVersions.length > 0}
        <div class="grid grid-cols-2 md:grid-cols-3 lg:grid-cols-4 gap-2 max-h-[400px] overflow-y-auto pr-2">
          {#each cappedRemote as ver (ver.number)}
            <div class="flex items-center justify-between p-3 rounded-lg border transition-all bg-[var(--color-bg)]/50 {ver.updateFor ? 'border-amber-500/30' : 'border-[var(--color-border)] hover:border-primary-500/30'}">
              <div class="flex flex-col gap-1 min-w-0">
                <span class="font-mono text-xs font-bold truncate">{ver.number}</span>
                <div class="flex gap-1">
                  {#if ver.stable}
                    <span class="text-[9px] px-1 bg-emerald-500/10 text-emerald-500 rounded border border-emerald-500/20 uppercase font-bold">{$t('runtimes.labelStable')}</span>
                  {/if}
                  {#if ver.installed}
                    <span class="text-[9px] px-1 bg-blue-500/10 text-blue-500 rounded border border-blue-500/20 uppercase font-bold">{$t('runtimes.labelInstalled')}</span>
                  {:else if ver.updateFor}
                    <span class="text-[9px] px-1 bg-amber-500/10 text-amber-600 dark:text-amber-400 rounded border border-amber-500/20 uppercase font-bold" title="{ver.updateFor} → {ver.number}">{$t('runtimes.labelUpdate')}</span>
                  {/if}
                </div>
              </div>
              {#if !ver.installed}
                {#if ver.updateFor}
                  <button
                    class="btn-icon bg-amber-500 hover:bg-amber-600 text-white shadow-sm h-8 w-8"
                    on:click={() => update(ver.updateFor || '', ver.number)}
                    disabled={installing !== ''}
                    title="{ver.updateFor} → {ver.number} — {$t('runtimes.updateHint')}"
                  >
                    {#if installing === ver.number}
                      <div class="w-3 h-3 border-2 border-white border-t-transparent rounded-full animate-spin"></div>
                    {:else}
                      <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="17 8 12 3 7 8"/><line x1="12" y1="3" x2="12" y2="15"/></svg>
                    {/if}
                  </button>
                {:else}
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
              {/if}
            </div>
          {/each}
        </div>
        {#if !showAllVersions && visibleRemote.length > VERSION_ROW_CAP}
          <div class="text-center mt-3">
            <button class="text-xs text-primary-500 hover:underline" on:click={() => showAllVersions = true}>{$t('runtimes.showAll', String(visibleRemote.length))}</button>
          </div>
        {/if}
      {:else if loadingRemote}
        <div class="text-center py-12">
          <div class="w-8 h-8 border-3 border-primary-500 border-t-transparent rounded-full animate-spin mx-auto mb-4"></div>
          <p class="text-sm text-[var(--color-text-secondary)]">{$t('runtimes.loadingVersions').replace('{0}', activeLabel)}</p>
        </div>
      {:else}
        <div class="text-center py-12 text-[var(--color-text-secondary)]">
          <p class="text-sm">{$t('runtimes.emptyAvailable')}</p>
        </div>
      {/if}
    </div>
  </div>
</div>

<ImportCenter open={showImportCenter} on:close={() => showImportCenter = false} />
<RuntimeCatalog
  open={showRuntimeCatalog}
  on:close={() => showRuntimeCatalog = false}
  on:changed={() => loadRuntimeCatalog()}
  on:installed={(e) => { showRuntimeCatalog = false; loadRuntimeCatalog().then(() => switchTab(e.detail.name)); }}
/>

<ConfirmDialog
  open={pendingUninstall !== null}
  danger={true}
  busy={uninstallingVersion !== null}
  title={pendingUninstall ? $t('runtimes.confirmUninstallTitle', activeLabel, pendingUninstall) : ''}
  message={$t('runtimes.confirmUninstallMsg')}
  confirmLabel={uninstallingVersion ? $t('runtimes.uninstalling') : $t('runtimes.uninstall')}
  on:confirm={confirmUninstall}
  on:cancel={cancelUninstall}
/>
