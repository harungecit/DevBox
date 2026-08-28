<script lang="ts">
  import { onMount } from 'svelte';
  import { t, locale, loadLocale } from '../lib/i18n/index';
  import { theme, applyTheme, appConfig, loadConfig } from '../lib/stores/app';
  import {
    SetLanguage, SetTheme, SetAutoStart, IsAutoStartEnabled, SetStartMinimized, SetCloseToTray,
    GetDataDir, OpenDataDir, GetMigrationNotice, Quit,
    GetCloudflareStatus, VerifyCloudflareToken, ConfigureCloudflare, DisconnectCloudflare,
    GetAppVersion, CheckForUpdate, GetLastUpdateCheck, InstallAppUpdate, OpenInBrowser,
  } from '../../wailsjs/go/main/App';
  import { EventsOn } from '../../wailsjs/runtime/runtime';
  import { onDestroy } from 'svelte';
  import AppIcon from '../lib/components/AppIcon.svelte';

  // App updates
  interface ReleaseInfo { current: string; latest: string; available: boolean; url: string; assetUrl: string; notes: string; checkedAt: string; error: string }
  let appVersion = '';
  let release: ReleaseInfo | null = null;
  let checkingUpdate = false;
  let updateProgress: { percent: number; message: string } | null = null;
  let unsubs: Array<() => void> = [];

  async function checkUpdate() {
    checkingUpdate = true;
    try { release = await CheckForUpdate(); } catch (e) { errorMessage = String(e); }
    checkingUpdate = false;
  }

  async function installUpdate() {
    updateProgress = { percent: 0, message: '' };
    try { await InstallAppUpdate(); } catch (e) { errorMessage = String(e); updateProgress = null; }
  }

  onDestroy(() => { unsubs.forEach(f => f()); });

  let autoStartEnabled = false;
  let startMinimized = false;
  let closeToTray = true;
  let dataDir = '';
  let migration: { migrated: boolean; from: string; to: string } | null = null;
  let errorMessage = '';

  // Cloudflare
  interface CFStatus { configured: boolean; accountName: string; zoneName: string; tunnelName: string; connected: boolean; routes: number }
  let cf: CFStatus = { configured: false, accountName: '', zoneName: '', tunnelName: '', connected: false, routes: 0 };
  let cfToken = '';
  let cfAccounts: { id: string; name: string }[] = [];
  let cfZones: { id: string; name: string }[] = [];
  let cfAccountId = '';
  let cfZoneId = '';
  let cfBusy = false;
  let cfVerified = false;

  onMount(async () => {
    await loadConfig();
    startMinimized = $appConfig.startMinimized;
    closeToTray = $appConfig.closeToTray;
    try { autoStartEnabled = await IsAutoStartEnabled(); } catch { autoStartEnabled = $appConfig.autoStart; }
    try { dataDir = await GetDataDir(); } catch { dataDir = $appConfig.dataDir; }
    try { const m = await GetMigrationNotice(); if (m?.migrated) migration = m; } catch { /* ignore */ }
    try { appVersion = await GetAppVersion(); } catch { /* ignore */ }
    try { const r = await GetLastUpdateCheck(); if (r?.checkedAt) release = r; } catch { /* ignore */ }
    unsubs.push(EventsOn('appupdate:progress', (d: any) => { updateProgress = { percent: d?.percent ?? 0, message: d?.message ?? '' }; }));
    unsubs.push(EventsOn('appupdate:error', (d: any) => { updateProgress = null; errorMessage = d?.error || 'update failed'; }));
    await loadCloudflare();
  });

  async function loadCloudflare() {
    try { cf = await GetCloudflareStatus(); } catch { /* ignore */ }
  }

  async function changeLanguage(lang: string) {
    await SetLanguage(lang);
    await loadLocale(lang);
  }

  async function changeTheme(newTheme: string) {
    await SetTheme(newTheme);
    theme.set(newTheme);
    applyTheme(newTheme);
  }

  async function toggleAutoStart() {
    const next = !autoStartEnabled;
    autoStartEnabled = next;
    try { await SetAutoStart(next); } catch (e) { autoStartEnabled = !next; errorMessage = String(e); }
  }

  async function toggleStartMinimized() {
    const next = !startMinimized;
    startMinimized = next;
    try { await SetStartMinimized(next); } catch (e) { startMinimized = !next; errorMessage = String(e); }
  }

  async function toggleCloseToTray() {
    const next = !closeToTray;
    closeToTray = next;
    try { await SetCloseToTray(next); } catch (e) { closeToTray = !next; errorMessage = String(e); }
  }

  async function verifyToken() {
    cfBusy = true; errorMessage = ''; cfVerified = false;
    try {
      const r = await VerifyCloudflareToken(cfToken);
      cfAccounts = r.accounts || [];
      cfZones = r.zones || [];
      cfAccountId = cfAccounts[0]?.id || '';
      cfZoneId = cfZones[0]?.id || '';
      cfVerified = true;
    } catch (e) {
      errorMessage = String(e);
    }
    cfBusy = false;
  }

  async function linkCloudflare() {
    const acc = cfAccounts.find(a => a.id === cfAccountId);
    const zone = cfZones.find(z => z.id === cfZoneId);
    if (!acc || !zone) return;
    cfBusy = true; errorMessage = '';
    try {
      await ConfigureCloudflare(cfToken, acc.id, acc.name, zone.id, zone.name);
      cfToken = ''; cfVerified = false;
      await loadCloudflare();
    } catch (e) {
      errorMessage = String(e);
    }
    cfBusy = false;
  }

  async function unlinkCloudflare() {
    cfBusy = true; errorMessage = '';
    try { await DisconnectCloudflare(); await loadCloudflare(); } catch (e) { errorMessage = String(e); }
    cfBusy = false;
  }

  const themes = [
    { id: 'light', key: 'settings.themeLight', icon: 'M12 3v1m0 16v1m9-9h-1M4 12H3m15.36 6.36l-.7-.7M6.34 6.34l-.7-.7m12.72 0l-.7.7M6.34 17.66l-.7.7M12 8a4 4 0 100 8 4 4 0 000-8z' },
    { id: 'dark', key: 'settings.themeDark', icon: 'M21 12.79A9 9 0 1111.21 3 7 7 0 0021 12.79z' },
    { id: 'system', key: 'settings.themeSystem', icon: 'M3 5a2 2 0 012-2h14a2 2 0 012 2v10a2 2 0 01-2 2H5a2 2 0 01-2-2V5zm5 16h8m-4-4v4' },
  ];
</script>

<div class="space-y-5 max-w-3xl">
  <div>
    <h2 class="text-2xl font-bold">{$t('settings.title')}</h2>
    <p class="text-[var(--color-text-secondary)] mt-1">{$t('settings.subtitle')}</p>
  </div>

  {#if errorMessage}
    <div class="p-3 rounded-lg bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 text-red-700 dark:text-red-400 text-sm flex justify-between gap-3">
      <span class="break-all">{errorMessage}</span>
      <button class="underline flex-shrink-0" on:click={() => errorMessage = ''}>{$t('common.dismiss')}</button>
    </div>
  {/if}

  {#if migration}
    <div class="p-3 rounded-lg bg-blue-500/5 border border-blue-500/20 text-sm text-[var(--color-text-secondary)]">
      {$t('settings.migratedNotice', migration.from, migration.to)}
    </div>
  {/if}

  <!-- General: language + appearance as two compact rows -->
  <div class="card !p-0 divide-y divide-[var(--color-border)]">
    <div class="flex items-center justify-between px-5 py-3.5">
      <span class="text-sm font-medium">{$t('settings.language')}</span>
      <div class="flex rounded-lg border border-[var(--color-border)] p-0.5 bg-[var(--color-bg)]">
        {#each [['en', 'English'], ['tr', 'Türkçe']] as [id, label]}
          <button
            class="px-3 py-1 text-xs font-semibold rounded-md transition-colors {$locale === id ? 'bg-primary-500 text-white shadow-sm' : 'text-[var(--color-text-secondary)] hover:text-[var(--color-text)]'}"
            on:click={() => changeLanguage(id)}
          >{label}</button>
        {/each}
      </div>
    </div>
    <div class="flex items-center justify-between px-5 py-3.5">
      <span class="text-sm font-medium">{$t('settings.theme')}</span>
      <div class="flex rounded-lg border border-[var(--color-border)] p-0.5 bg-[var(--color-bg)]">
        {#each themes as th}
          <button
            class="px-3 py-1 text-xs font-semibold rounded-md transition-colors flex items-center gap-1.5 {$theme === th.id ? 'bg-primary-500 text-white shadow-sm' : 'text-[var(--color-text-secondary)] hover:text-[var(--color-text)]'}"
            on:click={() => changeTheme(th.id)}
            title={$t(th.key)}
          >
            <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d={th.icon}/></svg>
            {$t(th.key).replace(/ (Theme|Tema)$/i, '').replace(/ Teması$/i, '')}
          </button>
        {/each}
      </div>
    </div>
  </div>

  <!-- Behavior toggles -->
  <div class="card !p-0 divide-y divide-[var(--color-border)]">
    {#each [
      { label: 'settings.autoStart', desc: 'settings.autoStartDesc', value: autoStartEnabled, toggle: toggleAutoStart },
      { label: 'settings.startMinimized', desc: 'settings.startMinimizedDesc', value: startMinimized, toggle: toggleStartMinimized },
      { label: 'settings.closeToTray', desc: 'settings.closeToTrayDesc', value: closeToTray, toggle: toggleCloseToTray },
    ] as row}
      <div class="flex items-center justify-between px-5 py-3.5 gap-6">
        <div class="min-w-0">
          <p class="text-sm font-medium">{$t(row.label)}</p>
          <p class="text-xs text-[var(--color-text-secondary)] mt-0.5">{$t(row.desc)}</p>
        </div>
        <button
          class="relative inline-flex items-center h-6 w-11 rounded-full transition-all flex-shrink-0 {row.value ? 'bg-primary-500' : 'bg-slate-200 dark:bg-slate-700'}"
          on:click={row.toggle}
          role="switch"
          aria-checked={row.value}
        >
          <span class="inline-block h-5 w-5 rounded-full bg-white shadow-md transition-transform {row.value ? 'translate-x-5' : 'translate-x-0.5'}"></span>
        </button>
      </div>
    {/each}
  </div>

  <!-- Storage -->
  <div class="card !p-0">
    <div class="flex items-center justify-between px-5 py-3.5 gap-6">
      <div class="min-w-0">
        <p class="text-sm font-medium">{$t('settings.dataDir')}</p>
        <p class="text-xs text-[var(--color-text-secondary)] mt-0.5">{$t('settings.dataDirDesc')}</p>
        <p class="font-mono text-xs text-primary-500 mt-1 truncate">{dataDir}</p>
      </div>
      <button class="btn-secondary text-xs flex-shrink-0" on:click={() => OpenDataDir()}>{$t('settings.openFolder')}</button>
    </div>
  </div>

  <!-- App updates -->
  <div class="card p-5">
    <div class="flex items-start justify-between gap-4">
      <div class="min-w-0">
        <h3 class="text-sm font-semibold">{$t('update.title')}</h3>
        <p class="text-xs text-[var(--color-text-secondary)] mt-0.5">{$t('update.desc')}</p>
        <p class="text-xs mt-2">
          <span class="text-[var(--color-text-secondary)]">{$t('update.current')}:</span>
          <span class="font-mono font-bold">v{appVersion}</span>
          {#if release && !release.error}
            {#if release.available}
              <span class="ml-2 text-[10px] px-1.5 py-0.5 rounded bg-amber-500/10 text-amber-600 dark:text-amber-400 border border-amber-500/20 font-bold">{$t('update.available', 'v' + release.latest)}</span>
            {:else}
              <span class="ml-2 text-[10px] text-emerald-500 font-bold uppercase">{$t('update.upToDate')}</span>
            {/if}
          {/if}
        </p>
        {#if release?.error}<p class="text-[11px] text-red-500 mt-1 font-mono">{release.error}</p>{/if}
        {#if release?.checkedAt}<p class="text-[10px] text-[var(--color-text-secondary)] mt-1">{$t('update.lastChecked', new Date(release.checkedAt).toLocaleString())}</p>{/if}
        {#if updateProgress}
          <div class="mt-2 w-64">
            <div class="h-1.5 rounded-full bg-slate-200 dark:bg-slate-700 overflow-hidden"><div class="h-full bg-primary-500 transition-all" style="width:{updateProgress.percent}%"></div></div>
            <p class="text-[10px] text-[var(--color-text-secondary)] mt-1">{$t('update.downloading')} {updateProgress.message}</p>
          </div>
        {/if}
      </div>
      <div class="flex items-center gap-2 flex-shrink-0">
        {#if release?.available}
          {#if release.url}<button class="text-xs text-primary-500 hover:underline" on:click={() => OpenInBrowser(release?.url || '')}>{$t('update.notes')}</button>{/if}
          <button class="btn-primary text-xs" on:click={installUpdate} disabled={updateProgress !== null || !release.assetUrl}>{$t('update.install')}</button>
        {/if}
        <button class="btn-secondary text-xs" on:click={checkUpdate} disabled={checkingUpdate}>
          {#if checkingUpdate}<div class="w-3 h-3 border-2 border-current border-t-transparent rounded-full animate-spin inline-block mr-1"></div>{$t('update.checking')}{:else}{$t('update.check')}{/if}
        </button>
      </div>
    </div>
  </div>

  <!-- Cloudflare -->
  <div class="card p-5">
    <div class="flex items-start justify-between gap-4 mb-3">
      <div>
        <h3 class="text-sm font-semibold flex items-center gap-2">
          <svg class="w-4 h-4 text-orange-500" viewBox="0 0 24 24" fill="currentColor"><path d="M16.5 17h-10a4.5 4.5 0 01-.8-8.93A6 6 0 0117.2 8.3 4.35 4.35 0 0116.5 17z"/></svg>
          {$t('settings.cloudflare')}
        </h3>
        <p class="text-xs text-[var(--color-text-secondary)] mt-0.5">{$t('settings.cloudflareDesc')}</p>
      </div>
      {#if cf.configured}
        <button class="text-xs px-3 py-1.5 rounded-lg text-red-500 border border-red-500/20 hover:bg-red-500/10 disabled:opacity-50 flex-shrink-0" on:click={unlinkCloudflare} disabled={cfBusy}>{$t('settings.cfDisconnect')}</button>
      {/if}
    </div>

    {#if cf.configured}
      <div class="text-xs space-y-1">
        <p class="font-medium">{$t('settings.cfLinked', cf.accountName, cf.zoneName)}</p>
        <p class="text-[var(--color-text-secondary)] font-mono">
          {$t('settings.cfTunnel', cf.tunnelName, cf.routes)} ·
          <span class={cf.connected ? 'text-emerald-500' : ''}>{cf.connected ? $t('settings.cfConnected') : $t('settings.cfIdle')}</span>
        </p>
      </div>
    {:else}
      <div class="flex gap-2">
        <input
          type="password"
          bind:value={cfToken}
          placeholder={$t('settings.cfTokenPlaceholder')}
          class="flex-1 px-3 py-2 rounded-lg border border-[var(--color-border)] bg-[var(--color-bg)] text-sm font-mono focus:outline-none focus:ring-2 focus:ring-primary-500/50"
          on:keydown={(e) => { if (e.key === 'Enter') verifyToken(); }}
        />
        <button class="btn-secondary text-xs" on:click={verifyToken} disabled={cfBusy || !cfToken.trim()}>
          {#if cfBusy && !cfVerified}<div class="w-3 h-3 border-2 border-current border-t-transparent rounded-full animate-spin inline-block mr-1"></div>{/if}
          {$t('settings.cfVerify')}
        </button>
      </div>
      <p class="text-[11px] text-[var(--color-text-secondary)] mt-2">{$t('settings.cfTokenHelp')}</p>

      {#if cfVerified}
        <div class="grid grid-cols-2 gap-3 mt-3">
          <div>
            <label for="cf-account" class="block text-[10px] font-bold uppercase tracking-wider text-[var(--color-text-secondary)] mb-1">{$t('settings.cfAccount')}</label>
            <select id="cf-account" class="w-full px-2 py-1.5 text-xs rounded-lg border border-[var(--color-border)] bg-[var(--color-bg)]" bind:value={cfAccountId}>
              {#each cfAccounts as a}<option value={a.id}>{a.name}</option>{/each}
            </select>
          </div>
          <div>
            <label for="cf-zone" class="block text-[10px] font-bold uppercase tracking-wider text-[var(--color-text-secondary)] mb-1">{$t('settings.cfZone')}</label>
            <select id="cf-zone" class="w-full px-2 py-1.5 text-xs rounded-lg border border-[var(--color-border)] bg-[var(--color-bg)]" bind:value={cfZoneId}>
              {#each cfZones as z}<option value={z.id}>{z.name}</option>{/each}
            </select>
          </div>
        </div>
        <div class="flex justify-end mt-3">
          <button class="btn-primary text-xs" on:click={linkCloudflare} disabled={cfBusy || !cfAccountId || !cfZoneId}>
            {#if cfBusy}<div class="w-3 h-3 border-2 border-white border-t-transparent rounded-full animate-spin inline-block mr-1"></div>{/if}
            {$t('settings.cfConnect')}
          </button>
        </div>
      {/if}
    {/if}
  </div>

  <!-- About + Quit -->
  <div class="card !p-0">
    <div class="flex items-center justify-between px-5 py-4 gap-4">
      <div class="flex items-center gap-3 min-w-0">
        <div class="w-10 h-10 flex-shrink-0"><AppIcon size="w-10 h-10" /></div>
        <div class="min-w-0">
          <p class="text-sm font-bold">DevBox <span class="text-primary-500">v{appVersion}</span> <span class="font-normal text-[var(--color-text-secondary)]">· {$t('app.description')}</span></p>
          <p class="text-xs text-[var(--color-text-secondary)]">
            Harun Geçit ·
            <a href="https://harungecit.dev" target="_blank" class="text-primary-500 hover:underline">harungecit.dev</a> ·
            <a href="https://github.com/harungecit/DevBox" target="_blank" class="text-primary-500 hover:underline">GitHub</a>
          </p>
        </div>
      </div>
      <button class="text-xs px-3 py-1.5 rounded-lg text-red-500 border border-red-500/20 hover:bg-red-500/10 flex-shrink-0" on:click={() => Quit()} title={$t('settings.quitDesc')}>
        {$t('settings.quit')}
      </button>
    </div>
  </div>
</div>
