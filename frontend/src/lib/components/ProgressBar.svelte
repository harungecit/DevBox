<script lang="ts">
  export let percent: number = 0;
  export let message: string = '';

  $: indeterminate = percent < 0;
</script>

<div class="w-full">
  {#if message}
    <p class="text-xs text-[var(--color-text-secondary)] mb-1">{message}</p>
  {/if}
  <div class="w-full bg-surface-200 dark:bg-surface-700 rounded-full h-2 overflow-hidden">
    {#if indeterminate}
      <div class="bg-primary-500 h-2 rounded-full indeterminate-bar"></div>
    {:else}
      <div
        class="bg-primary-500 h-2 rounded-full transition-all duration-300 ease-out"
        style="width: {Math.min(100, Math.max(0, percent))}%"
      ></div>
    {/if}
  </div>
  {#if !indeterminate}
    <p class="text-xs text-[var(--color-text-secondary)] mt-1 text-right">{percent}%</p>
  {/if}
</div>

<style>
  .indeterminate-bar {
    width: 40%;
    animation: indeterminate 1.5s ease-in-out infinite;
  }

  @keyframes indeterminate {
    0% {
      transform: translateX(-100%);
    }
    50% {
      transform: translateX(150%);
    }
    100% {
      transform: translateX(-100%);
    }
  }
</style>
