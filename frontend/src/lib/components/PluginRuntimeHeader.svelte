<script lang="ts">
  import { createEventDispatcher } from 'svelte';
  import { t } from '../i18n/index';
  import { runtimeLogo } from '../logos';
  import type { RuntimeMeta } from '../stores/runtimes';
  import { BrowserOpenURL } from '../../../wailsjs/runtime/runtime';
  import { UpdateVfoxPlugin, RemoveVfoxPlugin } from '../../../wailsjs/go/main/App';
  import { pluginInstalls, startPluginJob, clearPluginJob } from '../stores/installs';
  import ConfirmDialog from './ConfirmDialog.svelte';

  // Header card shown on a plugin runtime's tab: where the plugin comes from,
  // its notes, the environment variables it manages, update/remove actions.
  export let meta: RuntimeMeta;
  export let busy: boolean = false;
  const dispatch = createEventDispatcher();

  let error: string = '';
  let showNotes: boolean = false;
  $: job = $pluginInstalls[meta.name];
  $: envEntries = Object.entries(meta.envVars || {}).sort((a, b) => a[0].localeCompare(b[0]));

  async function update() {
    error = '';
    startPluginJob(meta.name, 'update');
    try {
      await UpdateVfoxPlugin(meta.name);
    } catch (e) {
      error = String(e);
      clearPluginJob(meta.name);
    }
  }

  let pendingRemove: boolean = false;
  let removing: boolean = false;
  let needsForce: boolean = false;

  function requestRemove() {
    pendingRemove = true;
    needsForce = meta.installed > 0;
  }

  async function confirmRemove() {
    removing = true;
    error = '';
    try {
      await RemoveVfoxPlugin(meta.name, needsForce);
      pendingRemove = false;
      dispatch('removed', { name: meta.name });
    } catch (e) {
      error = String(e);
      pendingRemove = false;
    }
    removing = false;
  }

  function cancelRemove() {
    if (removing) return;
    pendingRemove = false;
  }
</script>

<div class="card p-5">
  <div class="flex items-start justify-between gap-4">
    <div class="flex items-start gap-3 min-w-0">
      <div class="w-10 h-10 rounded-lg overflow-hidden shrink-0 p-1 bg-white dark:bg-slate-800 shadow-sm">{@html runtimeLogo(meta.name, meta.displayName)}</div>
      <div class="min-w-0">
        <div class="flex items-center gap-2 flex-wrap">
          <h3 class="text-base font-bold">{meta.displayName}</h3>
          <span class="text-[9px] px-1.5 py-0.5 rounded bg-primary-500/10 text-primary-500 border border-primary-500/20 uppercase font-bold">{$t('runtimes.pluginTab')}</span>
          {#if meta.pluginVersion}
            <span class="font-mono text-[10px] text-[var(--color-text-secondary)]">{$t('plugins.pluginVersion', meta.pluginVersion)}</span>
          {/if}
          {#if meta.license}
            <span class="text-[10px] text-[var(--color-text-secondary)]">{meta.license}</span>
          {/if}
          {#if meta.pluginUpdate}
            <span class="text-[9px] px-1 bg-amber-500/10 text-amber-600 dark:text-amber-400 rounded border border-amber-500/20 uppercase font-bold">{$t('plugins.updateAvailable')} · v{meta.pluginUpdate}</span>
          {/if}
        </div>
        {#if meta.description}
          <p class="text-xs text-[var(--color-text-secondary)] mt-0.5">{meta.description}</p>
        {/if}
        <div class="flex items-center gap-3 mt-1.5 text-[10px] text-[var(--color-text-secondary)] flex-wrap">
          {#if meta.homepage}
            <button class="hover:text-primary-500 hover:underline flex items-center gap-1" on:click={() => BrowserOpenURL(meta.homepage)}>
              <svg class="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6"/><polyline points="15 3 21 3 21 9"/><line x1="10" y1="14" x2="21" y2="3"/></svg>
              {$t('plugins.homepage')}
            </button>
          {/if}
          <button class="hover:text-primary-500 hover:underline" on:click={() => BrowserOpenURL('https://vfox.dev')}>{$t('plugins.poweredBy')}</button>
          {#if meta.notes && meta.notes.length > 0}
            <button class="hover:text-primary-500 hover:underline" on:click={() => showNotes = !showNotes}>{$t('plugins.notes')} ({meta.notes.length})</button>
          {/if}
          <span class="italic" title={$t('plugins.thirdParty')}>{$t('plugins.thirdPartyShort')}</span>
        </div>
      </div>
    </div>
    <div class="flex items-center gap-2 shrink-0">
      {#if job && !job.error}
        <span class="text-[11px] text-primary-500 flex items-center gap-1.5">
          <div class="w-3 h-3 border-2 border-primary-500 border-t-transparent rounded-full animate-spin"></div>
          {job.message || $t('plugins.updating')}
        </span>
      {:else}
        {#if meta.pluginUpdate}
          <button class="text-xs px-3 py-1.5 rounded-lg font-medium bg-amber-500 hover:bg-amber-600 text-white shadow-sm disabled:opacity-50" on:click={update} disabled={busy}>{$t('plugins.update')}</button>
        {/if}
        <button class="text-xs px-3 py-1.5 rounded-lg font-medium text-red-500 border border-red-500/20 hover:bg-red-500/10 disabled:opacity-50" on:click={requestRemove} disabled={busy}>{$t('plugins.remove')}</button>
      {/if}
    </div>
  </div>

  {#if error || (job && job.error)}
    <div class="mt-3 p-2.5 rounded-lg bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 text-red-700 dark:text-red-400 text-xs">
      {error || job?.error}
      <button class="ml-2 underline" on:click={() => { error = ''; clearPluginJob(meta.name); }}>{$t('common.dismiss')}</button>
    </div>
  {/if}

  {#if showNotes && meta.notes && meta.notes.length > 0}
    <div class="mt-3 p-3 rounded-lg bg-amber-500/5 border border-amber-500/20 text-xs">
      <div class="text-[10px] font-bold uppercase tracking-wider text-amber-600 dark:text-amber-400 mb-1">{$t('plugins.notes')}</div>
      <ul class="list-disc pl-4 space-y-0.5 text-[var(--color-text-secondary)]">
        {#each meta.notes as n}<li>{n}</li>{/each}
      </ul>
    </div>
  {/if}

  {#if envEntries.length > 0}
    <div class="mt-3 pt-3 border-t border-[var(--color-border)]">
      <div class="flex items-center gap-2 mb-1.5">
        <span class="text-[10px] font-bold uppercase tracking-wider text-[var(--color-text-secondary)]">{$t('plugins.envVars')}</span>
        <span class="text-[10px] text-[var(--color-text-secondary)]">— {$t('plugins.envVarsHint')}</span>
      </div>
      <div class="grid grid-cols-1 md:grid-cols-2 gap-1">
        {#each envEntries as [k, v]}
          <div class="flex items-center gap-2 px-2 py-1 rounded bg-[var(--color-bg)] text-[11px] min-w-0">
            <span class="font-mono font-bold shrink-0">{k}</span>
            <span class="font-mono text-[var(--color-text-secondary)] truncate" title={v}>{v}</span>
          </div>
        {/each}
      </div>
    </div>
  {/if}
</div>

<ConfirmDialog
  open={pendingRemove}
  danger={true}
  busy={removing}
  title={$t('plugins.remove.confirm', meta.displayName)}
  message={needsForce ? $t('plugins.remove.hasVersions', String(meta.installed)) : $t('plugins.remove.msg')}
  confirmLabel={needsForce ? $t('plugins.removeWithVersions') : $t('plugins.remove')}
  on:confirm={confirmRemove}
  on:cancel={cancelRemove}
/>
