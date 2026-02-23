<script lang="ts">
  import { onMount } from 'svelte';
  import { t, locale, loadLocale } from '../lib/i18n/index';
  import { theme, applyTheme } from '../lib/stores/app';
  import { SetLanguage, SetTheme, SetAutoStart, GetConfig } from '../../wailsjs/go/main/App';
  import AppIcon from '../lib/components/AppIcon.svelte';

  let autoStartEnabled: boolean = false;

  onMount(async () => {
    const cfg = await GetConfig();
    autoStartEnabled = cfg.autoStart || false;
  });

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
    autoStartEnabled = !autoStartEnabled;
    try {
      await SetAutoStart(autoStartEnabled);
    } catch (e) {
      autoStartEnabled = !autoStartEnabled; // revert on error
    }
  }
</script>

<div class="space-y-6">
  <div>
    <h2 class="text-2xl font-bold">{$t('settings.title')}</h2>
    <p class="text-[var(--color-text-secondary)] mt-1">{$t('settings.subtitle')}</p>
  </div>

  <!-- Language -->
  <div class="card">
    <h3 class="text-sm font-semibold mb-4">{$t('settings.language')}</h3>
    <div class="flex gap-3">
      <button
        class="px-6 py-3 rounded-lg border-2 transition-all text-sm font-medium {$locale === 'en' ? 'border-primary-500 bg-primary-500/10 text-primary-500' : 'border-[var(--color-border)] text-[var(--color-text-secondary)] hover:border-surface-400'}"
        on:click={() => changeLanguage('en')}
      >
        <span class="text-lg block mb-1">EN</span>
        English
      </button>
      <button
        class="px-6 py-3 rounded-lg border-2 transition-all text-sm font-medium {$locale === 'tr' ? 'border-primary-500 bg-primary-500/10 text-primary-500' : 'border-[var(--color-border)] text-[var(--color-text-secondary)] hover:border-surface-400'}"
        on:click={() => changeLanguage('tr')}
      >
        <span class="text-lg block mb-1">TR</span>
        Türkçe
      </button>
    </div>
  </div>

  <!-- Theme -->
  <div class="card">
    <h3 class="text-sm font-semibold mb-4">{$t('settings.theme')}</h3>
    <div class="flex gap-3">
      <button
        class="flex-1 px-4 py-3 rounded-xl border-2 transition-all text-sm font-bold flex flex-col items-center gap-2 {$theme === 'light' ? 'border-primary-500 bg-primary-500/10 text-primary-500 shadow-sm' : 'border-[var(--color-border)] text-[var(--color-text-secondary)] hover:border-slate-300 dark:hover:border-slate-600'}"
        on:click={() => changeTheme('light')}
      >
        <svg class="w-6 h-6" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="5"/><line x1="12" y1="1" x2="12" y2="3"/><line x1="12" y1="21" x2="12" y2="23"/><line x1="4.22" y1="4.22" x2="5.64" y2="5.64"/><line x1="18.36" y1="18.36" x2="19.78" y2="19.78"/><line x1="1" y1="12" x2="3" y2="12"/><line x1="21" y1="12" x2="23" y2="12"/><line x1="4.22" y1="19.78" x2="5.64" y2="18.36"/><line x1="18.36" y1="5.64" x2="19.78" y2="4.22"/></svg>
        {$t('settings.themeLight')}
      </button>

      <button
        class="flex-1 px-4 py-3 rounded-xl border-2 transition-all text-sm font-bold flex flex-col items-center gap-2 {$theme === 'dark' ? 'border-primary-500 bg-primary-500/10 text-primary-500 shadow-sm' : 'border-[var(--color-border)] text-[var(--color-text-secondary)] hover:border-slate-300 dark:hover:border-slate-600'}"
        on:click={() => changeTheme('dark')}
      >
        <svg class="w-6 h-6" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z"/></svg>
        {$t('settings.themeDark')}
      </button>

      <button
        class="flex-1 px-4 py-3 rounded-xl border-2 transition-all text-sm font-bold flex flex-col items-center gap-2 {$theme === 'system' ? 'border-primary-500 bg-primary-500/10 text-primary-500 shadow-sm' : 'border-[var(--color-border)] text-[var(--color-text-secondary)] hover:border-slate-300 dark:hover:border-slate-600'}"
        on:click={() => changeTheme('system')}
      >
        <svg class="w-6 h-6" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="2" y="3" width="20" height="14" rx="2" ry="2"/><line x1="8" y1="21" x2="16" y2="21"/><line x1="12" y1="17" x2="12" y2="21"/></svg>
        {$t('settings.themeSystem')}
      </button>
    </div>
  </div>

  <!-- Auto Start -->
  <div class="card p-6">
    <div class="flex items-center justify-between">
      <div class="flex items-center gap-4">
        <div class="p-2.5 bg-blue-500/10 rounded-xl text-blue-500">
          <svg class="w-6 h-6" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M13 2L3 14h9l-1 8 10-12h-9l1-8z"/></svg>
        </div>
        <div>
          <h3 class="text-sm font-bold">{$t('settings.autoStart')}</h3>
          <p class="text-xs text-[var(--color-text-secondary)] mt-0.5">{$t('settings.autoStartDesc')}</p>
        </div>
      </div>
      <button
        class="relative inline-flex items-center h-6 w-11 rounded-full transition-all {autoStartEnabled ? 'bg-primary-500 shadow-inner' : 'bg-slate-200 dark:bg-slate-700'}"
        on:click={toggleAutoStart}
      >
        <span class="inline-block h-5 w-5 rounded-full bg-white shadow-md transition-transform {autoStartEnabled ? 'translate-x-5' : 'translate-x-0.5'}"></span>
      </button>
    </div>
  </div>

  <!-- About -->
  <div class="card p-6 overflow-hidden relative">
    <div class="absolute top-0 right-0 p-8 opacity-5">
      <AppIcon size="w-32 h-32" mono={true} />
    </div>
    
    <div class="relative z-10">
      <h3 class="text-base font-bold mb-4">{$t('settings.about')}</h3>
      <div class="flex items-center gap-4 mb-6">
        <div class="w-16 h-16 rounded-2xl flex items-center justify-center shadow-lg">
          <AppIcon size="w-16 h-16" />
        </div>
        <div>
          <p class="text-lg font-black tracking-tight">DevBox <span class="text-primary-500">v0.1.0</span></p>
          <p class="text-xs text-[var(--color-text-secondary)] font-medium">{$t('app.description')}</p>
        </div>
      </div>

      <div class="space-y-4 pt-4 border-t border-[var(--color-border)]">
        <div>
          <p class="text-[10px] font-bold text-slate-400 uppercase tracking-widest mb-2">Developer</p>
          <div class="flex items-center gap-3">
             <div class="w-8 h-8 rounded-full bg-slate-100 dark:bg-slate-800 flex items-center justify-center font-bold text-xs">HG</div>
             <div>
               <p class="text-sm font-bold text-[var(--color-text)]">Harun Geçit</p>
               <a href="https://harungecit.dev" target="_blank" class="text-xs text-primary-500 hover:underline">harungecit.dev</a>
             </div>
          </div>
        </div>

        <div class="flex gap-3">
          <a href="https://github.com/harungecit/DevBox" target="_blank" class="flex items-center gap-2 px-3 py-1.5 rounded-lg bg-slate-100 dark:bg-slate-800 text-xs font-bold hover:bg-slate-200 dark:hover:bg-slate-700 transition-colors">
            <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M9 19c-5 1.5-5-2.5-7-3m14 6v-3.87a3.37 3.37 0 0 0-.94-2.61c3.14-.35 6.44-1.54 6.44-7A5.44 5.44 0 0 0 20 4.77 5.07 5.07 0 0 0 19.91 1S18.73.65 16 2.48a13.38 13.38 0 0 0-7 0C6.27.65 5.09 1 5.09 1A5.07 5.07 0 0 0 5 4.77a5.44 5.44 0 0 0-1.5 3.78c0 5.42 3.3 6.61 6.44 7A3.37 3.37 0 0 0 9 18.13V22"></path></svg>
            GitHub
          </a>
        </div>
      </div>
    </div>
  </div>
</div>
