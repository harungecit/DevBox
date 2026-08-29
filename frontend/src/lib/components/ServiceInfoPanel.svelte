<script lang="ts">
  import { t } from '../i18n/index';
  import {
    GetServiceDetails,
    OpenFileInEditor,
    OpenInBrowser,
    SetServiceSetting,
  } from '../../../wailsjs/go/main/App';

  export let serviceName: string = '';
  export let onClose: () => void = () => {};

  interface ConfigFileEntry {
    label: string;
    path: string;
  }

  interface ConnectionEntry {
    label: string;
    value: string;
    key?: string; // editable setting: port | user | password | databases
  }

  interface WebLinkEntry {
    label: string;
    url: string;
  }

  interface ServiceDetailInfo {
    configFiles: ConfigFileEntry[];
    connectionInfo: ConnectionEntry[];
    webLinks: WebLinkEntry[];
  }

  let details: ServiceDetailInfo | null = null;
  let loading = true;
  let copiedField: string = '';

  async function loadDetails() {
    loading = true;
    try {
      details = await GetServiceDetails(serviceName);
    } catch (e) {
      console.error('Failed to load service details:', e);
      details = null;
    }
    loading = false;
  }

  async function copyValue(value: string, label: string) {
    try {
      await navigator.clipboard.writeText(value);
      copiedField = label;
      setTimeout(() => { copiedField = ''; }, 1500);
    } catch (e) {
      console.error('Copy failed:', e);
    }
  }

  // Inline editing of connection settings (port, user, password, databases)
  let editingKey = '';
  let editValue = '';
  let saving = false;
  let editError = '';

  function startEdit(entry: ConnectionEntry) {
    editingKey = entry.key || '';
    editValue = entry.value === '(none)' ? '' : (entry.key === 'databases' ? entry.value.split(' ')[0] : entry.value);
    editError = '';
  }
  function cancelEdit() { editingKey = ''; editError = ''; }
  async function saveEdit() {
    if (!editingKey) return;
    saving = true; editError = '';
    try {
      await SetServiceSetting(serviceName, editingKey, editValue);
      editingKey = '';
      await loadDetails();
    } catch (e: any) {
      editError = typeof e === 'string' ? e : e?.message || String(e);
    }
    saving = false;
  }
  function editKeydown(e: KeyboardEvent) {
    if (e.key === 'Enter') saveEdit();
    else if (e.key === 'Escape') cancelEdit();
  }

  async function openEditor(path: string) {
    try {
      await OpenFileInEditor(path);
    } catch (e) {
      console.error('Failed to open file:', e);
    }
  }

  async function openBrowser(url: string) {
    try {
      await OpenInBrowser(url);
    } catch (e) {
      console.error('Failed to open URL:', e);
    }
  }

  $: if (serviceName) loadDetails();
</script>

<div class="mt-4 pt-4 border-t border-[var(--color-border)]">
  {#if loading}
    <div class="flex items-center gap-2 py-4">
      <div class="w-4 h-4 border-2 border-primary-500 border-t-transparent rounded-full animate-spin"></div>
      <span class="text-sm text-[var(--color-text-secondary)]">{$t('common.loading')}</span>
    </div>
  {:else if details}
    <div class="space-y-4">
      <!-- Connection Info -->
      {#if details.connectionInfo && details.connectionInfo.length > 0}
        <div>
          <h4 class="text-xs font-bold text-[var(--color-text-secondary)] uppercase tracking-wider mb-2">
            {$t('services.connectionInfo')}
          </h4>
          <div class="bg-[var(--color-bg)] rounded-lg border border-[var(--color-border)] divide-y divide-[var(--color-border)]">
            {#each details.connectionInfo as entry}
              <div class="flex items-center justify-between px-3 py-2 group">
                <div class="flex items-center gap-3 min-w-0 flex-1">
                  <span class="text-xs text-[var(--color-text-secondary)] font-medium w-24 flex-shrink-0">{entry.label}</span>
                  {#if entry.key && editingKey === entry.key}
                    <input
                      class="text-xs font-mono font-bold px-2 py-0.5 rounded border border-primary-500/40 bg-[var(--color-card)] focus:outline-none focus:ring-2 focus:ring-primary-500/40 w-56"
                      type="text"
                      inputmode={entry.key === 'port' || entry.key === 'databases' ? 'numeric' : 'text'}
                      bind:value={editValue}
                      on:keydown={editKeydown}
                      disabled={saving}
                      autofocus
                    />
                    <button class="text-[10px] bg-primary-500 text-white px-2 py-0.5 rounded font-bold hover:bg-primary-600 disabled:opacity-50" on:click={saveEdit} disabled={saving}>
                      {#if saving}<div class="w-3 h-3 border-2 border-white border-t-transparent rounded-full animate-spin inline-block"></div>{:else}{$t('services.save')}{/if}
                    </button>
                    <button class="text-[10px] text-slate-400 hover:text-red-500 px-1" on:click={cancelEdit} disabled={saving}>✕</button>
                    {#if editError}<span class="text-[11px] text-red-500 truncate" title={editError}>{editError}</span>{/if}
                  {:else}
                    <span class="text-xs font-mono font-bold text-[var(--color-text)] truncate">{entry.value}</span>
                  {/if}
                </div>
                {#if entry.key && editingKey !== entry.key}
                  <button
                    class="text-xs px-2 py-0.5 rounded flex-shrink-0 ml-2 text-[var(--color-text-secondary)] hover:text-primary-500 hover:bg-primary-500/5 border border-transparent"
                    on:click={() => startEdit(entry)}
                    title={$t('services.editValue')}
                  >
                    <svg class="w-3 h-3 inline-block mr-0.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M12 20h9M16.5 3.5a2.121 2.121 0 013 3L7 19l-4 1 1-4L16.5 3.5z"/></svg>
                    {$t('services.editValue')}
                  </button>
                {/if}
                <button
                  class="text-xs px-2 py-0.5 rounded transition-all flex-shrink-0 ml-2 {copiedField === entry.label ? 'bg-emerald-500/10 text-emerald-500 border border-emerald-500/20' : 'text-[var(--color-text-secondary)] hover:text-primary-500 hover:bg-primary-500/5 border border-transparent'}"
                  on:click={() => copyValue(entry.value, entry.label)}
                  title={$t('services.copyValue')}
                >
                  {#if copiedField === entry.label}
                    <svg class="w-3 h-3 inline-block mr-0.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3"><polyline points="20 6 9 17 4 12"/></svg>
                    {$t('services.copied')}
                  {:else}
                    <svg class="w-3 h-3 inline-block mr-0.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="9" y="9" width="13" height="13" rx="2" ry="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>
                    {$t('services.copy')}
                  {/if}
                </button>
              </div>
            {/each}
          </div>
        </div>
      {/if}

      <!-- Config Files -->
      {#if details.configFiles && details.configFiles.length > 0}
        <div>
          <h4 class="text-xs font-bold text-[var(--color-text-secondary)] uppercase tracking-wider mb-2">
            {$t('services.configFiles')}
          </h4>
          <div class="space-y-1.5">
            {#each details.configFiles as file}
              <div class="flex items-center justify-between px-3 py-2 bg-[var(--color-bg)] rounded-lg border border-[var(--color-border)]">
                <div class="flex items-center gap-2 min-w-0">
                  <svg class="w-4 h-4 text-amber-500 flex-shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/></svg>
                  <div class="min-w-0">
                    <span class="text-xs font-bold block">{file.label}</span>
                    <span class="text-[10px] text-[var(--color-text-secondary)] font-mono truncate block">{file.path}</span>
                  </div>
                </div>
                <button
                  class="text-xs px-2.5 py-1 rounded-lg bg-amber-500/10 text-amber-600 dark:text-amber-400 hover:bg-amber-500/20 border border-amber-500/20 transition-all flex-shrink-0 ml-2 font-medium"
                  on:click={() => openEditor(file.path)}
                  title={$t('services.openInEditor')}
                >
                  <svg class="w-3 h-3 inline-block mr-0.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><path d="M12 20h9M16.5 3.5a2.121 2.121 0 0 1 3 3L7 19l-4 1 1-4L16.5 3.5z"/></svg>
                  {$t('services.openInEditor')}
                </button>
              </div>
            {/each}
          </div>
        </div>
      {/if}

      <!-- Web Links -->
      {#if details.webLinks && details.webLinks.length > 0}
        <div>
          <h4 class="text-xs font-bold text-[var(--color-text-secondary)] uppercase tracking-wider mb-2">
            {$t('services.webLinks')}
          </h4>
          <div class="flex flex-wrap gap-2">
            {#each details.webLinks as link}
              <button
                class="text-xs px-3 py-1.5 rounded-lg bg-primary-500/10 text-primary-500 hover:bg-primary-500/20 border border-primary-500/20 transition-all font-medium"
                on:click={() => openBrowser(link.url)}
                title={link.url}
              >
                <svg class="w-3 h-3 inline-block mr-1" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6"/><polyline points="15 3 21 3 21 9"/><line x1="10" y1="14" x2="21" y2="3"/></svg>
                {link.label}
              </button>
            {/each}
          </div>
        </div>
      {/if}
    </div>
  {:else}
    <p class="text-xs text-[var(--color-text-secondary)] py-4">{$t('services.noDetails')}</p>
  {/if}
</div>
