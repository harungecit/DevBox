<script lang="ts">
  import { createEventDispatcher } from 'svelte';
  import { t } from '../i18n/index';

  export let open: boolean = false;
  export let title: string = '';
  export let message: string = '';
  export let confirmLabel: string = '';
  export let cancelLabel: string = '';
  // `danger` switches the confirm button to a red destructive style.
  export let danger: boolean = false;
  // `busy` keeps the dialog open with a spinner on the confirm button while
  // the async operation runs, so the user sees something is happening.
  export let busy: boolean = false;

  const dispatch = createEventDispatcher<{ confirm: void; cancel: void }>();

  function onCancel() {
    if (busy) return;
    dispatch('cancel');
  }

  function onConfirm() {
    if (busy) return;
    dispatch('confirm');
  }

  function onBackdropKey(e: KeyboardEvent) {
    if (e.key === 'Escape') onCancel();
  }
</script>

{#if open}
  <div
    class="fixed inset-0 bg-black/50 flex items-center justify-center z-50"
    role="presentation"
    on:click={onCancel}
    on:keydown={onBackdropKey}
  >
    <div
      class="bg-[var(--color-card)] rounded-xl border border-[var(--color-border)] shadow-2xl w-[420px] p-6"
      role="dialog"
      aria-modal="true"
      on:click|stopPropagation
      on:keydown|stopPropagation
    >
      <h3 class="text-lg font-bold mb-2">{title}</h3>
      <p class="text-sm text-[var(--color-text-secondary)] whitespace-pre-line mb-5">{message}</p>
      <div class="flex justify-end gap-2">
        <button
          class="px-4 py-2 text-sm rounded-lg border border-[var(--color-border)] hover:bg-[var(--color-bg)] disabled:opacity-50"
          on:click={onCancel}
          disabled={busy}
        >
          {cancelLabel || $t('common.cancel')}
        </button>
        <button
          class="px-4 py-2 text-sm rounded-lg text-white flex items-center gap-2 disabled:opacity-70 {danger ? 'bg-red-600 hover:bg-red-700' : 'bg-blue-600 hover:bg-blue-700'}"
          on:click={onConfirm}
          disabled={busy}
        >
          {#if busy}
            <div class="w-3 h-3 border-2 border-white border-t-transparent rounded-full animate-spin"></div>
          {/if}
          {confirmLabel || $t('common.confirm')}
        </button>
      </div>
    </div>
  </div>
{/if}
