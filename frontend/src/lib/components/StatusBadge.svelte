<script lang="ts">
  export let status: 'running' | 'stopped' | 'error' | 'active' = 'stopped';
  export let label: string = '';

  const statusClass: Record<string, string> = {
    running: 'badge-success',
    active: 'badge-success',
    stopped: 'badge-danger',
    error: 'badge-warning',
  };
</script>

<span class="badge {statusClass[status] || 'badge-info'}">
  <span class="w-1.5 h-1.5 rounded-full mr-1.5 {status === 'running' || status === 'active' ? 'bg-emerald-500' : status === 'stopped' ? 'bg-red-500' : 'bg-amber-500'}"></span>
  {label || status}
</span>
