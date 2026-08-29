<script lang="ts">
  import { t } from '../i18n/index';
  import { OpenInBrowser } from '../../../wailsjs/go/main/App';
  import { currentPage } from '../stores/app';

  export let tool: {
    id: string; name: string; group: string; runtime: string; kind: string;
    desc: string; homepage: string; port: number; forServices: string[];
    installed: boolean; version: string; running: boolean; url: string;
    available: boolean; serviceName: string;
  };
  export let busy = false;
  export let progress = '';
  export let letter = '';
  export let color = 'bg-slate-500/10 text-slate-500';
  export let onInstall: (id: string) => void;
  export let onUninstall: (id: string) => void;
  export let onOpen: (id: string) => void = () => {};
  export let onStop: (id: string) => void = () => {};

  const runtimeLabel: Record<string, string> = { python: 'Python', go: 'Go', rust: 'Rust', node: 'Node.js', php: 'PHP' };
  $: isWeb = tool.kind === 'npm';
  $: needsService = isWeb && !tool.serviceName;
  $: blocked = !tool.available || needsService;
  $: initial = letter || tool.name.charAt(0).toUpperCase();
</script>

<div class="flex items-center justify-between gap-3 p-3 rounded-lg border border-[var(--color-border)] bg-[var(--color-bg)]">
  <div class="flex items-start gap-3 min-w-0">
    <div class="w-10 h-10 rounded-lg {color} flex items-center justify-center font-bold text-sm flex-shrink-0">{initial}</div>
    <div class="min-w-0">
      <div class="flex items-center gap-2 flex-wrap">
        <span class="text-sm font-bold">{tool.name}</span>
        {#if tool.installed}
          <span class="text-[10px] px-1.5 py-0.5 bg-emerald-500/10 text-emerald-500 rounded border border-emerald-500/20 font-bold uppercase">{$t('tools.labelInstalled')}</span>
          {#if tool.version}<span class="text-xs font-mono text-[var(--color-text-secondary)]">v{tool.version}</span>{/if}
        {/if}
        {#if tool.running}
          <span class="text-[10px] px-1.5 py-0.5 bg-emerald-500/10 text-emerald-500 rounded border border-emerald-500/20 font-bold uppercase">{$t('tools.runningOnPort', tool.port)}</span>
        {/if}
        {#if isWeb && tool.serviceName}
          <span class="text-[10px] px-1.5 py-0.5 bg-sky-500/10 text-sky-500 rounded border border-sky-500/20 font-bold uppercase">{$t('tools.manages', tool.serviceName)}</span>
        {/if}
      </div>
      <p class="text-xs text-[var(--color-text-secondary)] mt-0.5">{$t(tool.desc)}</p>
      {#if !tool.available}
        <p class="text-[11px] text-amber-600 dark:text-amber-400 mt-1 flex items-center gap-1.5 flex-wrap">
          <span>{$t('tools.needsRuntime', runtimeLabel[tool.runtime] || tool.runtime)}</span>
          <button class="underline font-medium" on:click={() => currentPage.set('runtimes')}>{$t('tools.goInstallRuntime', runtimeLabel[tool.runtime] || tool.runtime)}</button>
        </p>
      {:else if needsService}
        <p class="text-[11px] text-amber-600 dark:text-amber-400 mt-1 flex items-center gap-1.5 flex-wrap">
          <span>{$t('tools.needsService', tool.forServices.join(' / '))}</span>
          <button class="underline font-medium" on:click={() => currentPage.set('services')}>{$t('tools.goInstallService', tool.forServices[0])}</button>
        </p>
      {/if}
      {#if busy && progress}
        <p class="text-[11px] font-mono text-[var(--color-text-secondary)] mt-1 truncate max-w-md" title={progress}>{progress}</p>
      {/if}
    </div>
  </div>
  <div class="flex items-center gap-2 flex-shrink-0">
    {#if tool.homepage}
      <button class="text-[11px] text-[var(--color-text-secondary)] hover:underline" on:click={() => OpenInBrowser(tool.homepage)}>{$t('tools.docs')}</button>
    {/if}
    {#if !tool.installed && blocked}
      <span class="text-[10px] px-1.5 py-0.5 rounded border border-[var(--color-border)] text-[var(--color-text-secondary)] font-bold uppercase">{$t('tools.unavailable')}</span>
    {:else if !tool.installed}
      <button class="text-xs px-3 py-1.5 rounded-lg bg-blue-600 text-white hover:bg-blue-700 disabled:opacity-50 flex items-center gap-1.5" on:click={() => onInstall(tool.id)} disabled={busy}>
        {#if busy}<div class="w-3 h-3 border-2 border-white border-t-transparent rounded-full animate-spin"></div>{$t('tools.installing')}{:else}{$t('tools.install')}{/if}
      </button>
    {:else}
      {#if isWeb}
        <button class="text-xs px-3 py-1.5 rounded-lg bg-blue-600 text-white hover:bg-blue-700 disabled:opacity-50" on:click={() => onOpen(tool.id)} disabled={busy || blocked}>{$t('tools.open')}</button>
        {#if tool.running}
          <button class="text-xs px-3 py-1.5 rounded-lg border border-[var(--color-border)] hover:bg-[var(--color-card)] disabled:opacity-50" on:click={() => onStop(tool.id)} disabled={busy}>{$t('tools.stopServer')}</button>
        {/if}
      {/if}
      <button class="btn-icon text-red-500 hover:bg-red-500/10" on:click={() => onUninstall(tool.id)} disabled={busy} title={$t('tools.uninstall')}>
        {#if busy}<div class="w-4 h-4 border-2 border-red-500 border-t-transparent rounded-full animate-spin"></div>{:else}<svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M3 6h18M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/></svg>{/if}
      </button>
    {/if}
  </div>
</div>
