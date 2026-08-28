<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { t } from '../lib/i18n/index';
  import StatusBadge from '../lib/components/StatusBadge.svelte';
  import ProgressBar from '../lib/components/ProgressBar.svelte';
  import ServiceInfoPanel from '../lib/components/ServiceInfoPanel.svelte';
  import ConfirmDialog from '../lib/components/ConfirmDialog.svelte';
  import { serviceLogos } from '../lib/logos';
  import {
    GetAllServices,
    GetServiceVersions,
    RefreshServiceVersions,
    InstallService,
    UpdateService,
    UninstallService,
    StartService,
    StopService,
    RestartService,
    GetServiceLogs,
    CheckPort,
    SetServicePort,
    GetAutoStartServices,
    SetServiceAutoStart,
  } from '../../wailsjs/go/main/App';
  import { EventsOn } from '../../wailsjs/runtime/runtime';
  import { serviceInstalls, startServiceInstall, clearServiceInstall } from '../lib/stores/installs';

  type ServiceState = 'running' | 'stopped' | 'not_installed' | 'error';

  interface ServiceInfo {
    name: string;
    displayName: string;
    status: ServiceState;
    port: number;
    version: string;
    installed: boolean;
    updateVersion?: string;
    latestMajor?: string;
  }

  interface AvailableVersion {
    version: string;
    label: string;
  }

  interface ServiceVariant {
    id: string;
    name: string;
  }

  interface ServiceDef {
    id: string;
    name: string;
    descKey: string;
    defaultPort: number;
    managed: boolean;
    variants?: ServiceVariant[];
  }

  interface ServiceCategory {
    id: string;
    labelKey: string;
    descKey: string;
    services: ServiceDef[];
  }

  const serviceDefinitions: ServiceCategory[] = [
    {
      id: 'web', labelKey: 'services.categoryWeb', descKey: 'services.categoryWebDesc',
      services: [
        {
          id: 'webserver', name: 'Web Server', descKey: 'services.webServerDesc',
          defaultPort: 8500, managed: true,
          variants: [
            { id: 'nginx', name: 'Nginx' },
            { id: 'apache', name: 'Apache' },
            { id: 'caddy', name: 'Caddy' },
          ]
        },
        // FrankenPHP is independent — it bundles its own webserver + PHP runtime,
        // and projects choose it explicitly. Lives in the same Web category but
        // is not part of the nginx/apache/caddy mutually-exclusive group.
        { id: 'frankenphp', name: 'FrankenPHP', descKey: 'services.frankenphpDesc', defaultPort: 8501, managed: true },
      ],
    },
    {
      id: 'db', labelKey: 'services.categoryDb', descKey: 'services.categoryDbDesc',
      services: [
        { id: 'postgres', name: 'PostgreSQL', descKey: 'services.postgresDesc', defaultPort: 5432, managed: true },
        {
          id: 'mysqlcompat', name: 'MySQL / MariaDB', descKey: 'services.mysqlCompatDesc',
          defaultPort: 3306, managed: true,
          variants: [
            { id: 'mysql', name: 'MySQL' },
            { id: 'mariadb', name: 'MariaDB' },
          ]
        },
        { id: 'mongodb', name: 'MongoDB', descKey: 'services.mongodbDesc', defaultPort: 27017, managed: true },
      ],
    },
    {
      id: 'cache', labelKey: 'services.categoryCache', descKey: 'services.categoryCacheDesc',
      services: [
        {
          id: 'kvcache', name: 'Redis / Valkey', descKey: 'services.kvCacheDesc',
          defaultPort: 6379, managed: true,
          variants: [
            { id: 'redis', name: 'Redis' },
            { id: 'valkey', name: 'Valkey' },
          ]
        },
      ],
    },
    {
      id: 'mail', labelKey: 'services.categoryMail', descKey: 'services.categoryMailDesc',
      services: [
        { id: 'mailpit', name: 'Mailpit', descKey: 'services.mailpitDesc', defaultPort: 1025, managed: true },
      ],
    },
  ];

  let serviceStatuses: Record<string, ServiceInfo> = {};
  let autoStartSvcs: string[] = [];
  let errorMessage: string = '';

  // Install state is sourced from the global store so progress survives page
  // unmount. `installing` stays as a derived string for compatibility with the
  // many UI checks below (isInstalling per row, button disabled flags, etc.).
  $: serviceInstallEntries = Object.entries($serviceInstalls);
  $: installing = serviceInstallEntries[0]?.[0] ?? '';
  $: currentServiceInstall = serviceInstallEntries[0]?.[1];
  $: progressPercent = currentServiceInstall?.percent ?? 0;
  $: progressMessage = currentServiceInstall?.message ?? '';
  let showLogs: string = '';
  let logLines: string[] = [];
  let showInfo: string = '';

  // Action loading state: tracks which service is busy and what it's doing
  let busyService: string = '';
  let busyAction: string = ''; // 'starting' | 'stopping' | 'restarting' | 'uninstalling'

  // Install dialog state
  let dialogSvc: ServiceDef | null = null;
  let showInstallDialog: boolean = false;
  let selectedVariant: string = '';
  let installVersions: AvailableVersion[] = [];
  let selectedVersion: string = '';
  let selectedPort: number = 0;
  let portAvailable: boolean = true;
  let portMessage: string = '';
  let loadingVersions: boolean = false;

  // Port edit state
  let editingPort: string = '';
  let editPortValue: number = 0;

  async function loadServices() {
    try {
      const all = await GetAllServices();
      serviceStatuses = all || {};
      autoStartSvcs = await GetAutoStartServices() || [];
    } catch (e) {
      console.error('Failed to load services:', e);
    }
  }

  async function toggleAutoStart(id: string) {
    const enabled = !autoStartSvcs.includes(id);
    try {
      await SetServiceAutoStart(id, enabled);
      autoStartSvcs = enabled
        ? [...autoStartSvcs, id]
        : autoStartSvcs.filter(s => s !== id);
    } catch (e) {
      errorMessage = `Auto-start toggle failed: ${e}`;
    }
  }

  function getStatusLabel(status: ServiceState): string {
    if (status === 'running') return $t('services.running');
    if (status === 'stopped') return $t('services.stopped');
    return $t('services.notInstalled');
  }

  function getStatusType(status: ServiceState): 'running' | 'stopped' | 'error' {
    if (status === 'running') return 'running';
    if (status === 'stopped') return 'stopped';
    return 'error';
  }

  // Load versions for selected variant
  async function loadVariantVersions(variantId: string, force: boolean = false) {
    loadingVersions = true;
    installVersions = [];
    selectedVersion = '';
    try {
      installVersions = (force ? await RefreshServiceVersions(variantId) : await GetServiceVersions(variantId)) || [];
      if (installVersions.length > 0) {
        selectedVersion = installVersions[0].version;
      }
    } catch (e) {
      errorMessage = `Failed to fetch versions: ${e}`;
    }
    loadingVersions = false;
  }

  // Open install dialog
  async function openInstallDialog(svc: ServiceDef) {
    dialogSvc = svc;
    showInstallDialog = true;
    selectedPort = svc.defaultPort;
    portAvailable = true;
    portMessage = '';

    if (svc.variants) {
      selectedVariant = svc.variants[0].id;
    } else {
      selectedVariant = svc.id;
    }

    await loadVariantVersions(selectedVariant);
    await checkPortStatus(selectedPort);
  }

  async function selectVariant(variantId: string) {
    selectedVariant = variantId;
    await loadVariantVersions(variantId);
  }

  async function checkPortStatus(port: number) {
    try {
      const status = await CheckPort(port);
      portAvailable = status.available;
      portMessage = status.available ? '' : status.message;
    } catch (e) {
      portAvailable = false;
      portMessage = String(e);
    }
  }

  async function confirmInstall() {
    if (!selectedVersion || !selectedVariant) return;

    const name = selectedVariant;
    showInstallDialog = false;
    dialogSvc = null;
    errorMessage = '';
    startServiceInstall(name, $t('services.starting'));

    try {
      // InstallService returns immediately (background goroutine on the backend).
      // Progress / completion / errors flow through service:* events into the store.
      await InstallService(name, selectedVersion, selectedPort);
    } catch (e: any) {
      errorMessage = `${name}: ${errorStr(e)}`;
      clearServiceInstall(name);
    }
  }

  function dismissInstallError() {
    if (installing) clearServiceInstall(installing);
  }

  // In-place update: `pendingUpdate` holds the service awaiting confirmation.
  let pendingUpdate: { id: string; to: string } | null = null;

  function requestUpdate(id: string, to: string) {
    pendingUpdate = { id, to };
  }

  async function confirmUpdate() {
    if (!pendingUpdate) return;
    const { id, to } = pendingUpdate;
    pendingUpdate = null;
    errorMessage = '';
    startServiceInstall(id, $t('services.updating'));
    try {
      await UpdateService(id, to);
    } catch (e: any) {
      errorMessage = `${id}: ${errorStr(e)}`;
      clearServiceInstall(id);
    }
  }

  function errorStr(e: any): string {
    return typeof e === 'string' ? e : e?.message || JSON.stringify(e);
  }

  // Uninstall confirmation: `pendingUninstall` is the service id awaiting
  // confirmation. The existing `busyService` + `busyAction` flags continue to
  // drive the per-row spinner during the actual deletion.
  let pendingUninstall: string | null = null;

  function requestUninstall(id: string) {
    pendingUninstall = id;
  }

  function cancelUninstall() {
    if (busyAction === 'uninstalling') return; // can't cancel mid-uninstall
    pendingUninstall = null;
  }

  async function confirmUninstall() {
    if (!pendingUninstall) return;
    const id = pendingUninstall;
    errorMessage = '';
    busyService = id;
    busyAction = 'uninstalling';
    try {
      await UninstallService(id);
      await loadServices();
    } catch (e: any) {
      errorMessage = `${id}: ${errorStr(e)}`;
    } finally {
      busyService = '';
      busyAction = '';
      pendingUninstall = null;
    }
  }

  async function start(id: string) {
    errorMessage = '';
    busyService = id;
    busyAction = 'starting';
    try {
      await StartService(id);
      await loadServices();
    } catch (e: any) {
      errorMessage = `${id}: ${errorStr(e)}`;
    } finally {
      busyService = '';
      busyAction = '';
    }
  }

  async function stop(id: string) {
    errorMessage = '';
    busyService = id;
    busyAction = 'stopping';
    try {
      await StopService(id);
      await loadServices();
    } catch (e: any) {
      errorMessage = `${id}: ${errorStr(e)}`;
    } finally {
      busyService = '';
      busyAction = '';
    }
  }

  async function restart(id: string) {
    errorMessage = '';
    busyService = id;
    busyAction = 'restarting';
    try {
      await RestartService(id);
      await loadServices();
    } catch (e: any) {
      errorMessage = `${id}: ${errorStr(e)}`;
    } finally {
      busyService = '';
      busyAction = '';
    }
  }

  async function viewLogs(id: string) {
    if (showLogs === id) { showLogs = ''; return; }
    showLogs = id;
    showInfo = '';
    try {
      logLines = await GetServiceLogs(id, 50) || [];
    } catch (e) {
      logLines = [`Error: ${e}`];
    }
  }

  function toggleInfo(id: string) {
    if (showInfo === id) { showInfo = ''; return; }
    showInfo = id;
    showLogs = '';
  }

  async function startEditPort(id: string) {
    editingPort = id;
    editPortValue = serviceStatuses[id]?.port || 0;
  }

  async function savePort(id: string) {
    try {
      await SetServicePort(id, editPortValue);
      await loadServices();
    } catch (e) {
      errorMessage = `${id}: ${e}`;
    }
    editingPort = '';
  }

  // Calling EventsOff(name) would nuke the global listener in installs.ts too,
  // so we keep per-listener unsub handles returned by EventsOn.
  let eventUnsubs: Array<() => void> = [];

  onMount(() => {
    // service:progress / service:installed / service:error are handled globally
    // in lib/stores/installs.ts so state survives unmount. We only listen for
    // service:installed here to refresh the service list while the page is open.
    eventUnsubs.push(EventsOn('service:installed', () => {
      loadServices();
    }));
    eventUnsubs.push(EventsOn('services:changed', () => {
      loadServices();
    }));
    eventUnsubs.push(EventsOn('versions:refreshed', () => {
      loadServices();
    }));
    loadServices();
  });

  onDestroy(() => {
    eventUnsubs.forEach(fn => fn());
    eventUnsubs = [];
  });
</script>

<div class="space-y-6">
  <div>
    <h2 class="text-2xl font-bold">{$t('services.title')}</h2>
    <p class="text-[var(--color-text-secondary)] mt-1">{$t('services.subtitle')}</p>
  </div>

  {#if errorMessage}
    <div class="p-3 rounded-lg bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 text-red-700 dark:text-red-400 text-sm">
      <div class="flex items-start justify-between gap-2">
        <pre class="whitespace-pre-wrap font-mono text-xs leading-relaxed flex-1">{errorMessage}</pre>
        <button class="text-xs underline flex-shrink-0 mt-0.5" on:click={() => errorMessage = ''}>{$t('common.dismiss')}</button>
      </div>
    </div>
  {/if}

  {#if currentServiceInstall}
    {#if currentServiceInstall.error}
      <div class="p-3 rounded-lg bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 text-red-700 dark:text-red-400 text-sm">
        <div class="flex items-start justify-between gap-2">
          <pre class="whitespace-pre-wrap font-mono text-xs leading-relaxed flex-1"><span class="font-bold">{installing}:</span> {currentServiceInstall.error}</pre>
          <button class="text-xs underline flex-shrink-0 mt-0.5" on:click={dismissInstallError}>{$t('common.dismiss')}</button>
        </div>
      </div>
    {:else}
      <div class="card">
        <div class="flex items-center gap-3 mb-2">
          <div class="w-6 h-6">{@html serviceLogos[installing] || ''}</div>
          <span class="text-sm font-medium">{$t('runtimes.downloading')} {installing}</span>
        </div>
        <ProgressBar percent={currentServiceInstall.percent} message={currentServiceInstall.message} />
      </div>
    {/if}
  {/if}

  <!-- Install Dialog -->
  {#if showInstallDialog && dialogSvc}
    <div class="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
      <div class="bg-[var(--color-card)] rounded-xl border border-[var(--color-border)] shadow-2xl w-[460px] p-6">
        <h3 class="text-lg font-bold mb-1">{$t('services.install')}</h3>
        <p class="text-xs text-[var(--color-text-secondary)] mb-5">{$t(dialogSvc.descKey)}</p>

        <!-- Variant Selector (for grouped services) -->
        {#if dialogSvc.variants}
          <div class="mb-5">
            <span class="block text-sm font-medium mb-2">{$t('services.selectEngine')}</span>
            <div class="grid gap-2" style="grid-template-columns: repeat({dialogSvc.variants.length}, 1fr)">
              {#each dialogSvc.variants as variant}
                <button
                  class="p-3 rounded-lg border-2 transition-all flex flex-col items-center gap-2 {selectedVariant === variant.id ? 'border-primary-500 bg-primary-500/5' : 'border-[var(--color-border)] hover:border-primary-300'}"
                  on:click={() => selectVariant(variant.id)}
                >
                  <div class="w-10 h-10">{@html serviceLogos[variant.id] || ''}</div>
                  <span class="text-xs font-bold {selectedVariant === variant.id ? 'text-primary-500' : ''}">{variant.name}</span>
                </button>
              {/each}
            </div>
          </div>
        {/if}

        <!-- Version Selection -->
        <div class="mb-4">
          <div class="flex items-center justify-between mb-1.5">
            <label for="svc-version" class="block text-sm font-medium">{$t('settings.version')}</label>
            <button class="text-[11px] text-primary-500 hover:underline" on:click={() => loadVariantVersions(selectedVariant, true)} disabled={loadingVersions}>{$t('services.refreshVersions')}</button>
          </div>
          {#if loadingVersions}
            <div class="text-sm text-[var(--color-text-secondary)]">{$t('common.loading')}</div>
          {:else if installVersions.length > 0}
            <select
              id="svc-version"
              bind:value={selectedVersion}
              class="w-full px-3 py-2 rounded-lg border border-[var(--color-border)] bg-[var(--color-bg)] text-sm focus:outline-none focus:ring-2 focus:ring-primary-500/50"
            >
              {#each installVersions as ver}
                <option value={ver.version}>{ver.label}</option>
              {/each}
            </select>
          {:else}
            <p class="text-sm text-red-500">{$t('services.noVersions')}</p>
          {/if}
        </div>

        <!-- Port Selection -->
        <div class="mb-4">
          <label for="svc-port" class="block text-sm font-medium mb-1.5">{$t('services.port')}</label>
          <input
            id="svc-port"
            type="number"
            bind:value={selectedPort}
            on:change={() => checkPortStatus(selectedPort)}
            class="w-full px-3 py-2 rounded-lg border border-[var(--color-border)] bg-[var(--color-bg)] text-sm font-mono focus:outline-none focus:ring-2 focus:ring-primary-500/50"
            min="1"
            max="65535"
          />
          {#if !portAvailable}
            <div class="mt-1.5 flex items-start gap-2 p-2 rounded-lg bg-amber-50 dark:bg-amber-900/20 border border-amber-200 dark:border-amber-800">
              <svg class="w-4 h-4 text-amber-500 flex-shrink-0 mt-0.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <path d="M10.29 3.86L1.82 18a2 2 0 001.71 3h16.94a2 2 0 001.71-3L13.71 3.86a2 2 0 00-3.42 0z"/><line x1="12" y1="9" x2="12" y2="13"/><line x1="12" y1="17" x2="12.01" y2="17"/>
              </svg>
              <div>
                <p class="text-xs text-amber-700 dark:text-amber-400 font-medium">{portMessage}</p>
                <p class="text-xs text-amber-600 dark:text-amber-500 mt-0.5">{$t('services.portWarning')}</p>
              </div>
            </div>
          {:else if selectedPort > 0}
            <p class="mt-1 text-xs text-emerald-600 dark:text-emerald-400">{$t('services.portAvailable').replace('{0}', String(selectedPort))}</p>
          {/if}
        </div>

        <!-- Actions -->
        <div class="flex justify-end gap-2 mt-6">
          <button class="btn-secondary" on:click={() => { showInstallDialog = false; dialogSvc = null; }}>{$t('common.cancel')}</button>
          <button
            class="btn-primary"
            on:click={confirmInstall}
            disabled={!selectedVersion || loadingVersions}
          >
            {$t('services.install')}
          </button>
        </div>
      </div>
    </div>
  {/if}

  {#each serviceDefinitions as category}
    <div>
      <div class="mb-3">
        <h3 class="text-base font-semibold">{$t(category.labelKey)}</h3>
        <p class="text-xs text-[var(--color-text-secondary)]">{$t(category.descKey)}</p>
      </div>

      <div class="space-y-3">
        {#each category.services as svc}
          <!-- Compute active variant and effective IDs -->
          {@const activeVar = svc.variants?.find(v => serviceStatuses[v.id]?.installed) || null}
          {@const eid = activeVar?.id || svc.id}
          {@const info = serviceStatuses[eid]}
          {@const status = info?.status || 'not_installed'}
          {@const installed = info?.installed || false}
          {@const version = info?.version || '-'}
          {@const port = info?.port || svc.defaultPort}
          {@const displayName = activeVar ? activeVar.name : svc.name}
          {@const logoId = activeVar?.id || (svc.variants?.[0]?.id || svc.id)}
          {@const isInstalling = svc.variants ? svc.variants.some(v => v.id === installing) : installing === svc.id}
          {@const isBusy = busyService === eid}

          <div class="card p-4">
            <div class="flex items-center justify-between gap-6">
              <!-- Left: Logo & Info -->
              <div class="flex items-center gap-4 flex-1 min-w-0">
                {#if !installed && svc.variants}
                  <!-- Show all variant logos when not installed -->
                  <div class="flex -space-x-1.5 flex-shrink-0">
                    {#each svc.variants as v}
                      <div class="w-10 h-10 rounded-lg overflow-hidden border-2 border-[var(--color-card)] shadow-sm">
                        {@html serviceLogos[v.id] || ''}
                      </div>
                    {/each}
                  </div>
                {:else}
                  <!-- Show single logo -->
                  <div class="w-12 h-12 rounded-xl overflow-hidden flex-shrink-0 shadow-sm">
                    {@html serviceLogos[logoId] || ''}
                  </div>
                {/if}
                <div class="truncate">
                  <h4 class="font-bold text-base">{displayName}</h4>
                  <p class="text-xs text-[var(--color-text-secondary)] mt-0.5 truncate">{$t(svc.descKey)}</p>
                </div>
              </div>

              <!-- Middle: Status & Config (grid for consistent alignment) -->
              <div class="grid flex-shrink-0 items-center" style="grid-template-columns: auto auto; gap: 6px 12px;">
                <!-- Row 1, Col 1: Status -->
                {#if isBusy}
                  <div class="flex items-center gap-2 px-2.5 py-1 rounded-full bg-amber-500/10 border border-amber-500/20">
                    <div class="w-3 h-3 border-2 border-amber-500 border-t-transparent rounded-full animate-spin"></div>
                    <span class="text-[11px] font-bold text-amber-600 dark:text-amber-400">
                      {#if busyAction === 'starting'}{$t('services.starting')}
                      {:else if busyAction === 'stopping'}{$t('services.stopping')}
                      {:else if busyAction === 'restarting'}{$t('services.restarting')}
                      {:else if busyAction === 'uninstalling'}{$t('services.uninstalling')}
                      {:else}{$t('common.loading')}{/if}
                    </span>
                  </div>
                {:else}
                  <StatusBadge status={getStatusType(status)} label={getStatusLabel(status)} />
                {/if}

                <!-- Row 1, Col 2: Auto -->
                {#if installed && !isBusy}
                  <button
                    class="text-[10px] uppercase font-bold flex items-center gap-1 px-1.5 py-0.5 rounded transition-all justify-self-start {autoStartSvcs.includes(eid) ? 'bg-primary-500/10 text-primary-500 border border-primary-500/20' : 'text-slate-400 border border-transparent hover:border-slate-200 dark:hover:border-slate-700'}"
                    on:click={() => toggleAutoStart(eid)}
                    title={$t('services.autoStartToggle')}
                  >
                    <svg class="w-2.5 h-2.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3"><path d="M13 2L3 14h9l-1 8 10-12h-9l1-8z"/></svg>
                    {$t('services.labelAuto')}
                  </button>
                {:else}
                  <div></div>
                {/if}

                <!-- Row 2, Col 1: Port -->
                {#if installed}
                  {#if editingPort === eid}
                    <div class="flex items-center gap-1.5 p-1 px-2 border border-primary-500/30 bg-primary-500/5 rounded-lg shadow-inner col-span-2">
                      <span class="text-[10px] font-bold text-primary-500 uppercase tracking-tighter">{$t('services.labelPort')}</span>
                      <input
                        type="number"
                        bind:value={editPortValue}
                        class="w-16 px-1 py-0 text-xs font-mono bg-transparent border-0 focus:ring-0 text-primary-600 dark:text-primary-400 font-bold"
                        min="1" max="65535"
                      />
                      <button class="text-[10px] bg-primary-500 text-white px-2 py-0.5 rounded shadow-sm hover:bg-primary-600 transition-colors font-bold" on:click={() => savePort(eid)}>OK</button>
                      <button class="text-[10px] text-slate-400 hover:text-red-500 p-0.5" on:click={() => editingPort = ''}>✕</button>
                    </div>
                  {:else}
                    <button class="group flex items-center gap-1.5 px-2 py-1 rounded-lg border border-transparent hover:border-[var(--color-border)] hover:bg-[var(--color-bg)] transition-all" on:click={() => startEditPort(eid)} title={$t('services.configure')}>
                      <span class="text-[10px] text-[var(--color-text-secondary)] font-bold uppercase">{$t('services.labelPort')}</span>
                      <span class="font-mono text-xs font-bold text-primary-500 group-hover:text-primary-600 tabular-nums">{port}</span>
                      <svg class="w-3 h-3 text-slate-400 opacity-0 group-hover:opacity-100 transition-opacity" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><path d="M12 20h9M16.5 3.5a2.121 2.121 0 013 3L7 19l-4 1 1-4L16.5 3.5z"/></svg>
                    </button>

                    <!-- Row 2, Col 2: Version (+ update badge) -->
                    {#if version !== '-'}
                      <div class="flex items-center gap-1.5">
                        <div class="flex items-center gap-1.5 px-2 py-1 rounded-lg bg-slate-100 dark:bg-slate-800 border border-slate-200 dark:border-slate-700">
                          <span class="text-[10px] text-slate-500 font-bold uppercase">{$t('services.labelVer')}</span>
                          <span class="text-xs font-mono font-bold text-slate-600 dark:text-slate-300">{version}</span>
                        </div>
                        {#if info?.updateVersion}
                          <button
                            class="flex items-center gap-1 px-2 py-1 rounded-lg bg-amber-500/10 text-amber-600 dark:text-amber-400 border border-amber-500/20 hover:bg-amber-500/20 text-[10px] font-bold font-mono disabled:opacity-50"
                            on:click={() => requestUpdate(eid, info.updateVersion || '')}
                            disabled={isBusy || installing !== '' || busyService !== ''}
                            title={$t('services.updateTo', info.updateVersion)}
                          >
                            <svg class="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="17 8 12 3 7 8"/><line x1="12" y1="3" x2="12" y2="15"/></svg>
                            {info.updateVersion}
                          </button>
                        {:else if info?.latestMajor}
                          <span class="px-1.5 py-1 rounded-lg text-[10px] font-mono text-slate-400 border border-dashed border-slate-300 dark:border-slate-700" title={$t('services.majorAvailable', info.latestMajor)}>{info.latestMajor}</span>
                        {/if}
                      </div>
                    {:else}
                      <div></div>
                    {/if}
                  {/if}
                {:else}
                  <div class="flex items-center gap-1.5 px-2 py-1 text-xs text-[var(--color-text-secondary)] font-medium border border-dashed border-[var(--color-border)] rounded-lg">
                    <span class="text-[10px] font-bold uppercase">{$t('services.labelPort')}</span>
                    <span class="font-mono font-bold tabular-nums">{svc.defaultPort}</span>
                  </div>
                  <div></div>
                {/if}
              </div>

              <!-- Right: Actions Toolbar -->
              <div class="flex items-center gap-1.5 flex-shrink-0 min-w-fit justify-end">
                {#if !svc.managed}
                  <span class="text-xs text-[var(--color-text-secondary)] italic px-2 font-medium">{$t('common.comingSoon')}</span>
                {:else if status === 'not_installed'}
                  <button
                    class="btn-icon bg-blue-600 hover:bg-blue-700 text-white shadow-sm"
                    on:click={() => openInstallDialog(svc)}
                    disabled={installing !== '' || busyService !== ''}
                    title={$t('services.install')}
                  >
                    <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/></svg>
                  </button>
                {:else if status === 'stopped'}
                  <button
                    class="btn-icon bg-emerald-600 hover:bg-emerald-700 text-white shadow-sm"
                    on:click={() => start(eid)}
                    disabled={isBusy || installing !== '' || busyService !== ''}
                    title={$t('services.start')}
                  >
                    {#if isBusy && busyAction === 'starting'}
                      <div class="w-4 h-4 border-2 border-white border-t-transparent rounded-full animate-spin"></div>
                    {:else}
                      <svg class="w-4 h-4" viewBox="0 0 24 24" fill="currentColor"><path d="M5 3l14 9-14 9V3z"/></svg>
                    {/if}
                  </button>
                  <button
                    class="btn-icon bg-slate-200 dark:bg-slate-700 text-slate-600 dark:text-slate-300 hover:bg-slate-300 dark:hover:bg-slate-600"
                    on:click={() => startEditPort(eid)}
                    disabled={isBusy}
                    title={$t('services.configure')}
                  >
                    <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><path d="M12 20h9M16.5 3.5a2.121 2.121 0 0 1 3 3L7 19l-4 1 1-4L16.5 3.5z"/></svg>
                  </button>
                  <button
                    class="btn-icon {showInfo === eid ? 'bg-primary-500 text-white' : 'bg-slate-200 dark:bg-slate-700 text-slate-600 dark:text-slate-300 hover:bg-slate-300 dark:hover:bg-slate-600'}"
                    on:click={() => toggleInfo(eid)}
                    disabled={isBusy}
                    title={$t('services.info')}
                  >
                    <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><path d="M12 16v-4"/><path d="M12 8h.01"/></svg>
                  </button>
                  <button
                    class="btn-icon bg-slate-200 dark:bg-slate-700 text-slate-600 dark:text-slate-300 hover:bg-slate-300 dark:hover:bg-slate-600"
                    on:click={() => viewLogs(eid)}
                    disabled={isBusy}
                    title={$t('services.logs')}
                  >
                    <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/><line x1="16" y1="13" x2="8" y2="13"/><line x1="16" y1="17" x2="8" y2="17"/><polyline points="10 9 9 9 8 9"/></svg>
                  </button>
                  <button
                    class="btn-icon text-red-500 hover:bg-red-500/10"
                    on:click={() => requestUninstall(eid)}
                    disabled={isBusy || installing !== '' || busyService !== ''}
                    title={$t('common.delete')}
                  >
                    {#if isBusy && busyAction === 'uninstalling'}
                      <div class="w-4 h-4 border-2 border-red-500 border-t-transparent rounded-full animate-spin"></div>
                    {:else}
                      <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M3 6h18M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/></svg>
                    {/if}
                  </button>
                {:else}
                  <!-- Running -->
                  <button
                    class="btn-icon bg-red-600 hover:bg-red-700 text-white shadow-sm"
                    on:click={() => stop(eid)}
                    disabled={isBusy || busyService !== ''}
                    title={$t('services.stop')}
                  >
                    {#if isBusy && busyAction === 'stopping'}
                      <div class="w-4 h-4 border-2 border-white border-t-transparent rounded-full animate-spin"></div>
                    {:else}
                      <svg class="w-4 h-4" viewBox="0 0 24 24" fill="currentColor"><rect x="6" y="6" width="12" height="12"/></svg>
                    {/if}
                  </button>
                  <button
                    class="btn-icon bg-amber-500 hover:bg-amber-600 text-white shadow-sm"
                    on:click={() => restart(eid)}
                    disabled={isBusy || busyService !== ''}
                    title={$t('services.restart')}
                  >
                    {#if isBusy && busyAction === 'restarting'}
                      <div class="w-4 h-4 border-2 border-white border-t-transparent rounded-full animate-spin"></div>
                    {:else}
                      <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><path d="M23 4v6h-6M1 20v-6h6M3.51 9a9 9 0 0 1 14.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0 0 20.49 15"/></svg>
                    {/if}
                  </button>
                  <button
                    class="btn-icon bg-slate-200 dark:bg-slate-700 text-slate-600 dark:text-slate-300 hover:bg-slate-300 dark:hover:bg-slate-600"
                    on:click={() => startEditPort(eid)}
                    disabled={isBusy}
                    title={$t('services.configure')}
                  >
                    <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><path d="M12 20h9M16.5 3.5a2.121 2.121 0 0 1 3 3L7 19l-4 1 1-4L16.5 3.5z"/></svg>
                  </button>
                  <button
                    class="btn-icon {showInfo === eid ? 'bg-primary-500 text-white' : 'bg-slate-200 dark:bg-slate-700 text-slate-600 dark:text-slate-300 hover:bg-slate-300 dark:hover:bg-slate-600'}"
                    on:click={() => toggleInfo(eid)}
                    disabled={isBusy}
                    title={$t('services.info')}
                  >
                    <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><path d="M12 16v-4"/><path d="M12 8h.01"/></svg>
                  </button>
                  <button
                    class="btn-icon bg-slate-200 dark:bg-slate-700 text-slate-600 dark:text-slate-300 hover:bg-slate-300 dark:hover:bg-slate-600"
                    on:click={() => viewLogs(eid)}
                    disabled={isBusy}
                    title={$t('services.logs')}
                  >
                    <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/><line x1="16" y1="13" x2="8" y2="13"/><line x1="16" y1="17" x2="8" y2="17"/><polyline points="10 9 9 9 8 9"/></svg>
                  </button>
                {/if}
              </div>
            </div>

            {#if showLogs === eid}
              <div class="mt-4 pt-4 border-t border-[var(--color-border)]">
                <div class="bg-surface-900 dark:bg-surface-950 rounded-xl p-4 max-h-60 overflow-y-auto shadow-inner">
                  {#if logLines.length > 0}
                    {#each logLines as line}
                      <p class="font-mono text-xs text-surface-300 leading-relaxed py-0.5 border-b border-white/5 last:border-0">{line}</p>
                    {/each}
                  {:else}
                    <p class="font-mono text-xs text-surface-500 italic">{$t('services.noLogs')}</p>
                  {/if}
                </div>
              </div>
            {/if}

            {#if showInfo === eid}
              <ServiceInfoPanel serviceName={eid} onClose={() => showInfo = ''} />
            {/if}
          </div>
        {/each}
      </div>
    </div>
  {/each}
</div>

<ConfirmDialog
  open={pendingUninstall !== null}
  danger={true}
  busy={busyAction === 'uninstalling'}
  title={pendingUninstall ? $t('services.confirmUninstallTitle', serviceStatuses[pendingUninstall]?.displayName || pendingUninstall) : ''}
  message={$t('services.confirmUninstallMsg')}
  confirmLabel={busyAction === 'uninstalling' ? $t('services.uninstalling') : $t('common.delete')}
  on:confirm={confirmUninstall}
  on:cancel={cancelUninstall}
/>

<ConfirmDialog
  open={pendingUpdate !== null}
  danger={false}
  busy={false}
  title={pendingUpdate ? $t('services.confirmUpdateTitle', serviceStatuses[pendingUpdate.id]?.displayName || pendingUpdate.id, pendingUpdate.to) : ''}
  message={$t('services.confirmUpdateMsg')}
  confirmLabel={$t('services.update')}
  on:confirm={confirmUpdate}
  on:cancel={() => pendingUpdate = null}
/>
