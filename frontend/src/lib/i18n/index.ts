import { writable, derived } from 'svelte/store';
import { GetLocale, GetConfig } from '../../../wailsjs/go/main/App';

type Translations = Record<string, string>;

export const locale = writable<string>('en');
export const translations = writable<Translations>({});

// Derived store: translation function. Supports {0}, {1}, … positional
// placeholders that get replaced by trailing args. `$t('key', a, b)` substitutes
// `{0}` → a, `{1}` → b. No-arg calls keep working unchanged.
export const t = derived(
  translations,
  ($translations) => (key: string, ...args: (string | number)[]): string => {
    let msg = $translations[key] || key;
    for (let i = 0; i < args.length; i++) {
      msg = msg.split(`{${i}}`).join(String(args[i]));
    }
    return msg;
  }
);

// Load translations for a given language from Go backend
export async function loadLocale(lang: string) {
  try {
    const msgs = await GetLocale(lang);
    translations.set(msgs);
    locale.set(lang);
  } catch (e) {
    console.error('Failed to load locale:', lang, e);
  }
}

// Initialize i18n from saved config
export async function initI18n() {
  try {
    const cfg = await GetConfig();
    await loadLocale(cfg.language || 'en');
  } catch (e) {
    console.error('Failed to init i18n, using default:', e);
    await loadLocale('en');
  }
}
