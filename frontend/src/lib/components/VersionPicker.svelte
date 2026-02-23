<script lang="ts">
  import { t } from '../i18n/index';

  export let versions: string[] = [];
  export let activeVersion: string = '';
  export let onInstall: (version: string) => void = () => {};
  export let onSetGlobal: (version: string) => void = () => {};
  export let onUninstall: (version: string) => void = () => {};
</script>

<div class="space-y-2">
  {#each versions as version}
    <div class="flex items-center justify-between p-3 rounded-lg border border-[var(--color-border)] bg-[var(--color-bg)]">
      <div class="flex items-center gap-3">
        <span class="font-mono text-sm">{version}</span>
        {#if version === activeVersion}
          <span class="badge badge-success">{$t('runtimes.active')}</span>
        {/if}
      </div>
      <div class="flex items-center gap-2">
        {#if version !== activeVersion}
          <button class="btn-secondary text-xs" on:click={() => onSetGlobal(version)}>
            {$t('runtimes.setGlobal')}
          </button>
        {/if}
        <button class="text-xs px-3 py-1.5 rounded-lg text-red-500 hover:bg-red-50 dark:hover:bg-red-900/20 transition-colors" on:click={() => onUninstall(version)}>
          {$t('runtimes.uninstall')}
        </button>
      </div>
    </div>
  {/each}

  {#if versions.length === 0}
    <p class="text-sm text-[var(--color-text-secondary)] text-center py-4">
      {$t('dashboard.noRuntimes')}
    </p>
  {/if}
</div>
