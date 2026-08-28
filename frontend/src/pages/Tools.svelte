<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { t } from '../lib/i18n/index';
  import {
    IsAdminerInstalled,
    InstallAdminer,
    UninstallAdminer,
    OpenAdminer,
    StopAdminerServer,
    IsAdminerServerRunning,
    GetAdminerURL,
    DetectExternalDBTools,
    LaunchExternalTool,
    OpenInBrowser,
    IsComposerInstalled,
    InstallComposer,
    GetComposerVersion,
    GetGlobalRuntime,
    GetNpmVersion,
    GetNpmLatestVersion,
    UpdateNpm,
    IsYarnEnabled,
    EnableYarn,
    DisableYarn,
    GetYarnVersion,
    IsPnpmEnabled,
    EnablePnpm,
    DisablePnpm,
    GetPnpmVersion,
    IsBunInstalled,
    InstallBun,
    GetBunVersion,
    UninstallBun,
  } from '../../wailsjs/go/main/App';
  import { EventsOn } from '../../wailsjs/runtime/runtime';

  let errorMessage: string = '';
  let eventUnsubs: Array<() => void> = [];

  // --- Database Tools ---
  interface DetectedTool {
    id: string;
    name: string;
    path: string;
    installed: boolean;
  }
  let adminerInstalled = false;
  let adminerRunning = false;
  let adminerInstalling = false;
  let adminerBusy = false;
  let detectedTools: DetectedTool[] = [];

  const dbToolIds = new Set(['heidisql', 'dbeaver', 'pgadmin', 'compass']);
  const toolDownloadURLs: Record<string, string> = {
    heidisql: 'https://www.heidisql.com/download.php',
    dbeaver: 'https://dbeaver.io/download/',
    pgadmin: 'https://www.pgadmin.org/download/',
    compass: 'https://www.mongodb.com/products/tools/compass',
  };
  $: dbTools = detectedTools.filter(t => dbToolIds.has(t.id));

  async function loadDBTools() {
    try {
      adminerInstalled = await IsAdminerInstalled();
      adminerRunning = await IsAdminerServerRunning();
      detectedTools = (await DetectExternalDBTools()) || [];
    } catch (e) {
      // non-fatal
    }
  }

  async function installAdminer() {
    adminerInstalling = true;
    errorMessage = '';
    try {
      await InstallAdminer();
      adminerInstalled = true;
    } catch (e: any) {
      errorMessage = `Adminer: ${errorStr(e)}`;
    }
    adminerInstalling = false;
  }

  async function openAdminer() {
    adminerBusy = true;
    errorMessage = '';
    try {
      await OpenAdminer();
      adminerRunning = await IsAdminerServerRunning();
    } catch (e: any) {
      errorMessage = `Adminer: ${errorStr(e)}`;
    }
    adminerBusy = false;
  }

  async function stopAdminer() {
    adminerBusy = true;
    try {
      await StopAdminerServer();
      adminerRunning = false;
    } catch (e: any) {
      errorMessage = `Adminer: ${errorStr(e)}`;
    }
    adminerBusy = false;
  }

  async function uninstallAdminerHandler() {
    adminerBusy = true;
    try {
      await UninstallAdminer();
      adminerInstalled = false;
      adminerRunning = false;
    } catch (e: any) {
      errorMessage = `Adminer: ${errorStr(e)}`;
    }
    adminerBusy = false;
  }

  async function launchTool(toolId: string) {
    errorMessage = '';
    try {
      await LaunchExternalTool(toolId, '');
    } catch (e: any) {
      errorMessage = `${toolId}: ${errorStr(e)}`;
    }
  }

  function downloadTool(toolId: string) {
    const url = toolDownloadURLs[toolId];
    if (url) OpenInBrowser(url);
  }

  // --- Package Managers ---
  let activePhpVersion: string = '';
  let activeNodeVersion: string = '';

  // PHP / Composer
  let composerInstalled = false;
  let composerVersion = '';
  let installingComposer = false;

  // Node ecosystem
  let npmVersion = '';
  let npmLatest = '';
  let updatingNpm = false;
  $: npmUpdateAvailable = !!npmVersion && !!npmLatest && npmVersion !== npmLatest;
  let yarnEnabled = false;
  let yarnVersion = '';
  let enablingYarn = false;
  let disablingYarn = false;
  let pnpmEnabled = false;
  let pnpmVersion = '';
  let enablingPnpm = false;
  let disablingPnpm = false;

  // Bun (standalone, not tied to a Node version)
  let bunInstalled = false;
  let bunVersion = '';
  let installingBun = false;
  let uninstallingBun = false;

  async function loadPackageManagers() {
    try {
      activePhpVersion = await GetGlobalRuntime('php');
      activeNodeVersion = await GetGlobalRuntime('node');

      if (activePhpVersion) {
        composerInstalled = await IsComposerInstalled();
        if (composerInstalled) composerVersion = await GetComposerVersion();
      }

      if (activeNodeVersion) {
        npmVersion = await GetNpmVersion();
        GetNpmLatestVersion().then(v => npmLatest = v).catch(() => {});
        yarnEnabled = await IsYarnEnabled();
        if (yarnEnabled) yarnVersion = await GetYarnVersion();
        pnpmEnabled = await IsPnpmEnabled();
        if (pnpmEnabled) pnpmVersion = await GetPnpmVersion();
      }

      bunInstalled = await IsBunInstalled();
      if (bunInstalled) bunVersion = await GetBunVersion();
    } catch (e) {
      // non-fatal
    }
  }

  async function installComposer() {
    installingComposer = true;
    errorMessage = '';
    try {
      await InstallComposer();
    } catch (e: any) {
      errorMessage = `Composer: ${errorStr(e)}`;
      installingComposer = false;
    }
  }

  async function updateNpm() {
    updatingNpm = true;
    errorMessage = '';
    try {
      await UpdateNpm();
    } catch (e: any) {
      errorMessage = `npm: ${errorStr(e)}`;
      updatingNpm = false;
    }
  }

  async function enableYarn() {
    enablingYarn = true;
    errorMessage = '';
    try {
      await EnableYarn();
    } catch (e: any) {
      errorMessage = `Yarn: ${errorStr(e)}`;
      enablingYarn = false;
    }
  }

  async function disableYarn() {
    disablingYarn = true;
    try {
      await DisableYarn();
      yarnEnabled = false;
      yarnVersion = '';
    } catch (e: any) {
      errorMessage = `Yarn: ${errorStr(e)}`;
    }
    disablingYarn = false;
  }

  async function enablePnpm() {
    enablingPnpm = true;
    errorMessage = '';
    try {
      await EnablePnpm();
    } catch (e: any) {
      errorMessage = `pnpm: ${errorStr(e)}`;
      enablingPnpm = false;
    }
  }

  async function disablePnpm() {
    disablingPnpm = true;
    try {
      await DisablePnpm();
      pnpmEnabled = false;
      pnpmVersion = '';
    } catch (e: any) {
      errorMessage = `pnpm: ${errorStr(e)}`;
    }
    disablingPnpm = false;
  }

  async function installBun() {
    installingBun = true;
    errorMessage = '';
    try {
      await InstallBun();
    } catch (e: any) {
      errorMessage = `Bun: ${errorStr(e)}`;
      installingBun = false;
    }
  }

  async function uninstallBun() {
    uninstallingBun = true;
    try {
      await UninstallBun();
      bunInstalled = false;
      bunVersion = '';
    } catch (e: any) {
      errorMessage = `Bun: ${errorStr(e)}`;
    }
    uninstallingBun = false;
  }

  function errorStr(e: any): string {
    return typeof e === 'string' ? e : e?.message || JSON.stringify(e);
  }

  onMount(() => {
    // Composer / Bun / Yarn / pnpm fire IPC events on completion — handle here so
    // the Tools page shows up-to-date state without manual refresh.
    eventUnsubs.push(EventsOn('composer:installed', () => {
      installingComposer = false;
      composerInstalled = true;
      GetComposerVersion().then(v => composerVersion = v);
    }));
    eventUnsubs.push(EventsOn('composer:error', (data: any) => {
      installingComposer = false;
      errorMessage = `Composer: ${data?.error || 'install failed'}`;
    }));
    eventUnsubs.push(EventsOn('npm:updated', (data: any) => {
      updatingNpm = false;
      npmVersion = data?.version || npmVersion;
      GetNpmLatestVersion().then(v => npmLatest = v).catch(() => {});
    }));
    eventUnsubs.push(EventsOn('npm:error', (data: any) => {
      updatingNpm = false;
      errorMessage = `npm: ${data?.error || 'update failed'}`;
    }));
    eventUnsubs.push(EventsOn('bun:installed', () => {
      installingBun = false;
      bunInstalled = true;
      GetBunVersion().then(v => bunVersion = v);
    }));
    eventUnsubs.push(EventsOn('bun:error', (data: any) => {
      installingBun = false;
      errorMessage = `Bun: ${data?.error || 'install failed'}`;
    }));
    eventUnsubs.push(EventsOn('yarn:installed', () => {
      enablingYarn = false;
      yarnEnabled = true;
      GetYarnVersion().then(v => yarnVersion = v);
    }));
    eventUnsubs.push(EventsOn('yarn:error', (data: any) => {
      enablingYarn = false;
      errorMessage = `Yarn: ${data?.error || 'enable failed'}`;
    }));
    eventUnsubs.push(EventsOn('pnpm:installed', () => {
      enablingPnpm = false;
      pnpmEnabled = true;
      GetPnpmVersion().then(v => pnpmVersion = v);
    }));
    eventUnsubs.push(EventsOn('pnpm:error', (data: any) => {
      enablingPnpm = false;
      errorMessage = `pnpm: ${data?.error || 'enable failed'}`;
    }));

    loadDBTools();
    loadPackageManagers();
  });

  onDestroy(() => {
    eventUnsubs.forEach(fn => fn());
    eventUnsubs = [];
  });
</script>

<div class="space-y-6">
  <div>
    <h2 class="text-2xl font-bold">{$t('tools.title')}</h2>
    <p class="text-[var(--color-text-secondary)] mt-1">{$t('tools.subtitle')}</p>
  </div>

  {#if errorMessage}
    <div class="p-3 rounded-lg bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 text-red-700 dark:text-red-400 text-sm">
      <div class="flex items-start justify-between gap-2">
        <pre class="whitespace-pre-wrap font-mono text-xs leading-relaxed flex-1">{errorMessage}</pre>
        <button class="text-xs underline flex-shrink-0 mt-0.5" on:click={() => errorMessage = ''}>{$t('common.dismiss')}</button>
      </div>
    </div>
  {/if}

  <!-- Database Tools -->
  <div class="card p-5">
    <div class="mb-4">
      <h3 class="text-base font-bold">{$t('tools.dbToolsTitle')}</h3>
      <p class="text-xs text-[var(--color-text-secondary)]">{$t('tools.dbToolsSubtitle')}</p>
    </div>

    <div class="mb-4">
      <div class="text-[10px] font-bold uppercase tracking-wider text-[var(--color-text-secondary)] mb-2">{$t('tools.browserBased')}</div>
      <div class="flex items-center justify-between p-3 rounded-lg border border-[var(--color-border)] bg-[var(--color-bg)]">
        <div class="flex items-start gap-3 min-w-0">
          <div class="w-10 h-10 rounded-lg bg-blue-500/10 text-blue-500 flex items-center justify-center font-bold text-sm flex-shrink-0">A</div>
          <div class="min-w-0">
            <div class="flex items-center gap-2 flex-wrap">
              <span class="text-sm font-bold">Adminer</span>
              {#if adminerRunning}
                <span class="text-[10px] px-1.5 py-0.5 bg-emerald-500/10 text-emerald-500 rounded border border-emerald-500/20 font-bold uppercase">{$t('tools.runningOnPort', 8505)}</span>
              {/if}
            </div>
            <p class="text-xs text-[var(--color-text-secondary)] mt-0.5">{$t('tools.adminerDesc')}</p>
            {#if !adminerInstalled}
              <p class="text-[11px] text-[var(--color-text-secondary)] italic mt-1">{$t('tools.adminerNeedsPhp')}</p>
            {/if}
          </div>
        </div>
        <div class="flex items-center gap-2 flex-shrink-0">
          {#if !adminerInstalled}
            <button class="text-xs px-3 py-1.5 rounded-lg bg-blue-600 text-white hover:bg-blue-700 flex items-center gap-1.5 disabled:opacity-50" on:click={installAdminer} disabled={adminerInstalling}>
              {#if adminerInstalling}
                <div class="w-3 h-3 border-2 border-white border-t-transparent rounded-full animate-spin"></div>
                {$t('tools.installing')}
              {:else}
                {$t('tools.install')}
              {/if}
            </button>
          {:else}
            <button class="text-xs px-3 py-1.5 rounded-lg bg-blue-600 text-white hover:bg-blue-700 disabled:opacity-50" on:click={openAdminer} disabled={adminerBusy}>
              {$t('tools.open')}
            </button>
            {#if adminerRunning}
              <button class="text-xs px-3 py-1.5 rounded-lg border border-[var(--color-border)] hover:bg-[var(--color-card)] disabled:opacity-50" on:click={stopAdminer} disabled={adminerBusy}>
                {$t('tools.stopServer')}
              </button>
            {/if}
            <button class="text-xs px-2.5 py-1.5 rounded-lg text-red-500 hover:bg-red-500/10 border border-red-500/20 disabled:opacity-50" on:click={uninstallAdminerHandler} disabled={adminerBusy} title={$t('tools.uninstall')}>
              <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M3 6h18M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/></svg>
            </button>
          {/if}
        </div>
      </div>
    </div>

    {#if dbTools.length > 0}
      <div>
        <div class="text-[10px] font-bold uppercase tracking-wider text-[var(--color-text-secondary)] mb-2">{$t('tools.nativeApps')}</div>
        <div class="grid grid-cols-1 md:grid-cols-2 gap-2">
          {#each dbTools as tool}
            <div class="flex items-center justify-between p-3 rounded-lg border border-[var(--color-border)] bg-[var(--color-bg)]">
              <div class="flex items-center gap-2 min-w-0">
                <span class="text-sm font-bold truncate">{tool.name}</span>
                {#if tool.installed}
                  <span class="text-[10px] px-1.5 py-0.5 bg-emerald-500/10 text-emerald-500 rounded border border-emerald-500/20 font-bold uppercase whitespace-nowrap">{$t('tools.detected')}</span>
                {/if}
              </div>
              {#if tool.installed}
                <button class="text-xs px-2.5 py-1 rounded-lg bg-blue-600 text-white hover:bg-blue-700 flex-shrink-0" on:click={() => launchTool(tool.id)}>{$t('tools.launch')}</button>
              {:else}
                <button class="text-xs px-2.5 py-1 rounded-lg border border-[var(--color-border)] hover:bg-[var(--color-card)] flex-shrink-0" on:click={() => downloadTool(tool.id)} title={$t('tools.notDetected')}>{$t('tools.download')}</button>
              {/if}
            </div>
          {/each}
        </div>
      </div>
    {/if}
  </div>

  <!-- Package Managers -->
  <div class="card p-5">
    <div class="mb-4">
      <h3 class="text-base font-bold">{$t('tools.pkgMgrsTitle')}</h3>
      <p class="text-xs text-[var(--color-text-secondary)]">{$t('tools.pkgMgrsSubtitle')}</p>
    </div>

    <!-- PHP group -->
    <div class="mb-5">
      <div class="text-[10px] font-bold uppercase tracking-wider text-[var(--color-text-secondary)] mb-2">{$t('tools.phpGroup')}</div>

      <!-- Composer -->
      <div class="flex items-center justify-between p-3 rounded-lg border border-[var(--color-border)] bg-[var(--color-bg)]">
        <div class="flex items-start gap-3 min-w-0">
          <div class="w-10 h-10 rounded-lg bg-orange-500/10 text-orange-500 flex items-center justify-center font-bold text-sm flex-shrink-0">C</div>
          <div class="min-w-0">
            <div class="flex items-center gap-2 flex-wrap">
              <span class="text-sm font-bold">Composer</span>
              {#if composerInstalled}
                <span class="text-[10px] px-1.5 py-0.5 bg-emerald-500/10 text-emerald-500 rounded border border-emerald-500/20 font-bold uppercase">{$t('tools.labelInstalled')}</span>
                {#if composerVersion}
                  <span class="text-xs font-mono text-[var(--color-text-secondary)]">v{composerVersion}</span>
                {/if}
              {/if}
            </div>
            <p class="text-xs text-[var(--color-text-secondary)] mt-0.5">{$t('tools.composerDesc')}</p>
            {#if !activePhpVersion}
              <p class="text-[11px] text-[var(--color-text-secondary)] italic mt-1">{$t('tools.composerNeedsPhp')}</p>
            {/if}
          </div>
        </div>
        <div class="flex items-center gap-2 flex-shrink-0">
          {#if !composerInstalled}
            <button class="text-xs px-3 py-1.5 rounded-lg bg-blue-600 text-white hover:bg-blue-700 disabled:opacity-50 flex items-center gap-1.5" on:click={installComposer} disabled={installingComposer || !activePhpVersion}>
              {#if installingComposer}
                <div class="w-3 h-3 border-2 border-white border-t-transparent rounded-full animate-spin"></div>
                {$t('tools.installing')}
              {:else}
                {$t('tools.install')}
              {/if}
            </button>
          {/if}
        </div>
      </div>
    </div>

    <!-- Node.js group -->
    <div>
      <div class="text-[10px] font-bold uppercase tracking-wider text-[var(--color-text-secondary)] mb-2">{$t('tools.nodeGroup')}</div>

      {#if !activeNodeVersion}
        <p class="text-[11px] text-[var(--color-text-secondary)] italic mb-2">{$t('tools.pkgMgrsNeedNode')}</p>
      {/if}

      <div class="space-y-2">
        <!-- npm (built-in) -->
        <div class="flex items-center justify-between p-3 rounded-lg border border-[var(--color-border)] bg-[var(--color-bg)]">
          <div class="flex items-start gap-3 min-w-0">
            <div class="w-10 h-10 rounded-lg bg-red-500/10 text-red-500 flex items-center justify-center font-bold text-sm flex-shrink-0">n</div>
            <div class="min-w-0">
              <div class="flex items-center gap-2 flex-wrap">
                <span class="text-sm font-bold">npm</span>
                <span class="text-[10px] px-1.5 py-0.5 bg-emerald-500/10 text-emerald-500 rounded border border-emerald-500/20 font-bold uppercase">{$t('tools.builtIn')}</span>
                {#if npmVersion}
                  <span class="text-xs font-mono text-[var(--color-text-secondary)]">v{npmVersion}</span>
                {/if}
                {#if npmUpdateAvailable}
                  <span class="text-[10px] px-1.5 py-0.5 bg-amber-500/10 text-amber-600 dark:text-amber-400 rounded border border-amber-500/20 font-bold font-mono">{$t('tools.npmLatest', npmLatest)}</span>
                {:else if npmVersion && npmLatest}
                  <span class="text-[10px] text-emerald-500 font-bold uppercase">{$t('tools.upToDate')}</span>
                {/if}
              </div>
              <p class="text-xs text-[var(--color-text-secondary)] mt-0.5">{$t('tools.npmDesc')}</p>
            </div>
          </div>
          <div class="flex items-center gap-2 flex-shrink-0">
            {#if npmUpdateAvailable}
              <button class="text-xs px-2.5 py-1 rounded-lg bg-amber-500 text-white hover:bg-amber-600 disabled:opacity-50" on:click={updateNpm} disabled={updatingNpm}>
                {#if updatingNpm}<div class="w-3 h-3 border-2 border-white border-t-transparent rounded-full animate-spin inline-block mr-1"></div>{$t('tools.updating')}{:else}{$t('tools.update')}{/if}
              </button>
            {/if}
          </div>
        </div>

        <!-- Yarn -->
        <div class="flex items-center justify-between p-3 rounded-lg border border-[var(--color-border)] bg-[var(--color-bg)]">
          <div class="flex items-start gap-3 min-w-0">
            <div class="w-10 h-10 rounded-lg bg-cyan-500/10 text-cyan-500 flex items-center justify-center font-bold text-sm flex-shrink-0">Y</div>
            <div class="min-w-0">
              <div class="flex items-center gap-2 flex-wrap">
                <span class="text-sm font-bold">Yarn</span>
                {#if yarnEnabled}
                  <span class="text-[10px] px-1.5 py-0.5 bg-emerald-500/10 text-emerald-500 rounded border border-emerald-500/20 font-bold uppercase">{$t('tools.labelEnabled')}</span>
                  {#if yarnVersion}
                    <span class="text-xs font-mono text-[var(--color-text-secondary)]">v{yarnVersion}</span>
                  {/if}
                {/if}
              </div>
              <p class="text-xs text-[var(--color-text-secondary)] mt-0.5">{$t('tools.yarnDesc')}</p>
            </div>
          </div>
          <div class="flex items-center gap-2 flex-shrink-0">
            {#if yarnEnabled}
              <button class="text-xs px-2.5 py-1 rounded-lg text-red-500 hover:bg-red-500/10 border border-red-500/20 disabled:opacity-50" on:click={disableYarn} disabled={disablingYarn}>
                {#if disablingYarn}<div class="w-3 h-3 border-2 border-red-500 border-t-transparent rounded-full animate-spin inline-block mr-1"></div>{/if}
                {$t('tools.disable')}
              </button>
            {:else}
              <button class="text-xs px-2.5 py-1 rounded-lg bg-blue-600 text-white hover:bg-blue-700 disabled:opacity-50" on:click={enableYarn} disabled={enablingYarn || !activeNodeVersion}>
                {#if enablingYarn}<div class="w-3 h-3 border-2 border-white border-t-transparent rounded-full animate-spin inline-block mr-1"></div>{/if}
                {$t('tools.enable')}
              </button>
            {/if}
          </div>
        </div>

        <!-- pnpm -->
        <div class="flex items-center justify-between p-3 rounded-lg border border-[var(--color-border)] bg-[var(--color-bg)]">
          <div class="flex items-start gap-3 min-w-0">
            <div class="w-10 h-10 rounded-lg bg-amber-500/10 text-amber-500 flex items-center justify-center font-bold text-sm flex-shrink-0">p</div>
            <div class="min-w-0">
              <div class="flex items-center gap-2 flex-wrap">
                <span class="text-sm font-bold">pnpm</span>
                {#if pnpmEnabled}
                  <span class="text-[10px] px-1.5 py-0.5 bg-emerald-500/10 text-emerald-500 rounded border border-emerald-500/20 font-bold uppercase">{$t('tools.labelEnabled')}</span>
                  {#if pnpmVersion}
                    <span class="text-xs font-mono text-[var(--color-text-secondary)]">v{pnpmVersion}</span>
                  {/if}
                {/if}
              </div>
              <p class="text-xs text-[var(--color-text-secondary)] mt-0.5">{$t('tools.pnpmDesc')}</p>
            </div>
          </div>
          <div class="flex items-center gap-2 flex-shrink-0">
            {#if pnpmEnabled}
              <button class="text-xs px-2.5 py-1 rounded-lg text-red-500 hover:bg-red-500/10 border border-red-500/20 disabled:opacity-50" on:click={disablePnpm} disabled={disablingPnpm}>
                {#if disablingPnpm}<div class="w-3 h-3 border-2 border-red-500 border-t-transparent rounded-full animate-spin inline-block mr-1"></div>{/if}
                {$t('tools.disable')}
              </button>
            {:else}
              <button class="text-xs px-2.5 py-1 rounded-lg bg-blue-600 text-white hover:bg-blue-700 disabled:opacity-50" on:click={enablePnpm} disabled={enablingPnpm || !activeNodeVersion}>
                {#if enablingPnpm}<div class="w-3 h-3 border-2 border-white border-t-transparent rounded-full animate-spin inline-block mr-1"></div>{/if}
                {$t('tools.enable')}
              </button>
            {/if}
          </div>
        </div>

        <!-- Bun -->
        <div class="flex items-center justify-between p-3 rounded-lg border border-[var(--color-border)] bg-[var(--color-bg)]">
          <div class="flex items-start gap-3 min-w-0">
            <div class="w-10 h-10 rounded-lg bg-pink-500/10 text-pink-500 flex items-center justify-center font-bold text-sm flex-shrink-0">B</div>
            <div class="min-w-0">
              <div class="flex items-center gap-2 flex-wrap">
                <span class="text-sm font-bold">Bun</span>
                {#if bunInstalled}
                  <span class="text-[10px] px-1.5 py-0.5 bg-emerald-500/10 text-emerald-500 rounded border border-emerald-500/20 font-bold uppercase">{$t('tools.labelInstalled')}</span>
                  {#if bunVersion}
                    <span class="text-xs font-mono text-[var(--color-text-secondary)]">v{bunVersion}</span>
                  {/if}
                {/if}
              </div>
              <p class="text-xs text-[var(--color-text-secondary)] mt-0.5">{$t('tools.bunDesc')}</p>
            </div>
          </div>
          <div class="flex items-center gap-2 flex-shrink-0">
            {#if bunInstalled}
              <button class="text-xs px-2.5 py-1 rounded-lg text-red-500 hover:bg-red-500/10 border border-red-500/20 disabled:opacity-50" on:click={uninstallBun} disabled={uninstallingBun}>
                {#if uninstallingBun}<div class="w-3 h-3 border-2 border-red-500 border-t-transparent rounded-full animate-spin inline-block mr-1"></div>{/if}
                {$t('tools.uninstall')}
              </button>
            {:else}
              <button class="text-xs px-2.5 py-1 rounded-lg bg-blue-600 text-white hover:bg-blue-700 disabled:opacity-50" on:click={installBun} disabled={installingBun}>
                {#if installingBun}<div class="w-3 h-3 border-2 border-white border-t-transparent rounded-full animate-spin inline-block mr-1"></div>{/if}
                {$t('tools.install')}
              </button>
            {/if}
          </div>
        </div>
      </div>
    </div>
  </div>
</div>
