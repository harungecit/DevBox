<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import ConfirmDialog from '../lib/components/ConfirmDialog.svelte';
  import { t } from '../lib/i18n/index';
  import { EventsOn } from '../../wailsjs/runtime/runtime';
  import { runtimeCatalog, runtimeLabel } from '../lib/stores/runtimes';
  import ProgressBar from '../lib/components/ProgressBar.svelte';
  import {
    ListProjects,
    AddProject,
    RemoveProject,
    SelectProjectFolder,
    DetectFramework,
    SetupProjectDomain,
    ToggleProjectSSL,
    SetProjectPort,
    OpenProjectFolder,
    OpenInBrowser,
    OpenFileInEditor,
    GetProjectVhostPath,
    StartTunnel,
    StopTunnel,
    GetTunnelURL,
    GetRunningTunnels,
    GetWebServerPort,
    StartDevServer,
    StopDevServer,
    GetRunningDevServers,
    GetDevServerLogs,
    SetProjectStartCommand,
    SetProjectAutoStart,
    GetFrameworkCatalog,
    OpenProjectTerminal,
    SetProjectRuntime,
    SetProjectRuntimeVersion,
    SetProjectWebserver,
    SetProjectPublicHostname,
    GetProjectEnvHints,
    FixProjectEnvHints,
    GetDefaultProjectsDir,
    GetInstalledVersions,
    GetAllServices,
    GetProxyStatus,
    GetAvailableTemplates,
    ScaffoldNewProject,
    CloneGitProject,
    SelectParentFolder,
    IsGitInstalled,
  } from '../../wailsjs/go/main/App';

  interface ProjectInfo {
    name: string;
    path: string;
    domain: string;
    framework: string;
    ssl: boolean;
    port: number;
    startCommand: string;
    runtime?: string;        // php / node / go / python / rust / static
    runtimeVersion?: string; // pinned version; empty = use global active
    webserver?: string;      // auto / nginx / caddy / apache / frankenphp / devserver
    publicHostname?: string; // custom-domain tunnel hostname (needs linked Cloudflare)
    hostsRegistered?: boolean; // domain actually mapped in the hosts file
    autoStart?: boolean;       // keep the dev server running (start with DevBox, restart on crash)
  }

  interface TemplateInfo {
    id: string;
    name: string;
    category: string;
    requiredRuntime: string;
    requiresTool: string;
    available: boolean;
    runtimeVersion: string;
  }

  const appServerFrameworks = ['Next.js', 'Nuxt', 'Vue', 'React', 'Svelte', 'Angular', 'Go', 'Rust', 'Django', 'Python', 'Kemal', 'Crystal'];

  // Detected frameworks imply their runtime; offering PHP for a Next.js app only
  // produces broken configs. Mirrors project.RuntimeFromFramework on the Go side.
  const frameworkRuntime: Record<string, string> = {
    'Laravel': 'php', 'WordPress': 'php', 'Symfony': 'php', 'CodeIgniter': 'php', 'Yii': 'php', 'CakePHP': 'php', 'Drupal': 'php', 'PHP': 'php',
    'Next.js': 'node', 'Nuxt': 'node', 'Vue': 'node', 'React': 'node', 'Svelte': 'node', 'Angular': 'node',
    'Django': 'python', 'Python': 'python', 'Go': 'go', 'Rust': 'rust', 'Static': 'static',
    'Kemal': 'crystal', 'Crystal': 'crystal',
  };
  // Live catalog from the backend (project.Catalog); the static maps above are
  // only the fallback until it arrives.
  let catalog: Record<string, { runtime: string; appServer: boolean; port: number }> = {};
  async function loadCatalog() {
    try {
      const list = await GetFrameworkCatalog();
      const next: typeof catalog = {};
      for (const f of list || []) next[f.name] = { runtime: f.runtime, appServer: f.appServer, port: f.port };
      catalog = next;
      projects = projects; // re-evaluate isAppServer()/lockedRuntime() in the list
    } catch (e) {
      console.error('framework catalog:', e);
    }
  }
  function lockedRuntime(framework: string): string {
    return catalog[framework]?.runtime ?? frameworkRuntime[framework] ?? '';
  }
  function runtimeChoicesFor(framework: string, choices: { id: string; label: string }[]): { id: string; label: string }[] {
    const rt = lockedRuntime(framework);
    if (rt) return choices.filter((c) => c.id === rt);
    return choices;
  }

  async function toggleAutoStart(proj: ProjectInfo, on: boolean) {
    savingProjectSettings[proj.name] = true;
    savingProjectSettings = savingProjectSettings;
    try {
      await SetProjectAutoStart(proj.name, on);
      await loadProjects();
    } catch (e: any) {
      errorMessage = `${proj.name}: ${e?.message || e}`;
    }
    savingProjectSettings[proj.name] = false;
    savingProjectSettings = savingProjectSettings;
  }

  interface TunnelState {
    running: boolean;
    url: string;
    starting: boolean;
  }

  let projects: ProjectInfo[] = [];
  let loading = true;
  let errorMessage = '';

  // Add dialog state
  let showAddDialog = false;
  let newPath = '';
  let newDomain = '';
  let detectedFramework = '';
  let adding = false;

  // Action loading state
  let busyProject = '';
  let webServerPort = 0;

  // Per-project tunnel states
  let tunnelStates: Record<string, TunnelState> = {};
  let tunnelPollingIntervals: Record<string, ReturnType<typeof setInterval>> = {};

  // SSL toggle busy state
  let sslBusy: Record<string, boolean> = {};

  // Dev server states
  interface DevServerState {
    running: boolean;
    port: number;
    starting: boolean;
  }
  let devServerStates: Record<string, DevServerState> = {};

  // Per-project settings panel state
  let expandedProject: string | null = null;
  // Cache of installed versions per runtime so the version dropdown doesn't
  // re-fetch each time the user toggles the panel.
  let installedVersionsByRuntime: Record<string, { number: string }[]> = {};
  // Set of installed managed-webserver names (nginx/caddy/apache/frankenphp),
  // used to filter the Webserver dropdown to only options the user has ready.
  let installedWebservers: Set<string> = new Set();
  let savingProjectSettings: Record<string, boolean> = {};

  // Runtime choices come from the catalog (built-ins + plugin languages;
  // tool plugins such as kubectl are not project runtimes).
  $: runtimeChoices = [
    { id: '', label: $t('projects.runtimeAuto') },
    ...$runtimeCatalog.filter((m) => m.kind !== 'tool').map((m) => ({ id: m.name, label: m.displayName })),
    { id: 'static', label: 'Static' },
  ] as { id: string; label: string }[];

  // webserverChoicesFor returns the list of allowed webservers given a runtime.
  // App-server runtimes can only use their own dev server; PHP/Static can pick
  // any installed managed webserver.
  // `installed` is passed in (not read from module scope) so Svelte re-evaluates
  // the {@const} in the template once the async service list arrives.
  function webserverChoicesFor(rt: string, installed: Set<string>, current: string): { id: string; label: string }[] {
    // Anything that is not PHP/static serves itself (built-in app-server
    // runtimes and plugin runtimes alike).
    const isAppServer = rt !== '' && rt !== 'php' && rt !== 'static';
    if (isAppServer) {
      return [{ id: 'devserver', label: 'Dev server (built-in)' }];
    }
    // PHP / Static / unknown — allow any managed webserver the user has installed.
    const out: { id: string; label: string }[] = [
      { id: '', label: 'Auto (use installed default)' },
    ];
    for (const ws of ['nginx', 'caddy', 'apache', 'frankenphp']) {
      if (installed.has(ws) || ws === current) {
        out.push({ id: ws, label: ws.charAt(0).toUpperCase() + ws.slice(1) });
      }
    }
    return out;
  }

  async function loadInstalledVersionsFor(runtime: string) {
    if (!runtime || runtime === 'static' || installedVersionsByRuntime[runtime]) return;
    try {
      const list = await GetInstalledVersions(runtime);
      installedVersionsByRuntime[runtime] = (list || []).map((v: any) => ({ number: v.number }));
      installedVersionsByRuntime = installedVersionsByRuntime;
    } catch {
      installedVersionsByRuntime[runtime] = [];
    }
  }

  async function loadInstalledWebservers() {
    try {
      const all = await GetAllServices();
      const next = new Set<string>();
      for (const [name, info] of Object.entries(all || {})) {
        if ((info as any).installed) next.add(name);
      }
      installedWebservers = next;
    } catch {
      // non-fatal
    }
  }

  async function toggleSettingsPanel(proj: ProjectInfo) {
    if (expandedProject === proj.name) {
      expandedProject = null;
      return;
    }
    expandedProject = proj.name;
    await loadInstalledWebservers();
    if (proj.runtime) await loadInstalledVersionsFor(proj.runtime);
  }

  function selectValue(e: Event): string {
    return (e.currentTarget as HTMLSelectElement).value;
  }

  function inputValue(e: Event): string {
    return (e.currentTarget as HTMLInputElement).value;
  }

  function blurTarget(e: Event) {
    (e.currentTarget as HTMLInputElement).blur();
  }

  async function changeRuntime(proj: ProjectInfo, rt: string) {
    savingProjectSettings[proj.name] = true;
    savingProjectSettings = savingProjectSettings;
    try {
      await SetProjectRuntime(proj.name, rt);
      await loadProjects();
      if (rt) await loadInstalledVersionsFor(rt);
    } catch (e: any) {
      errorMessage = `${proj.name}: ${e?.message || e}`;
    }
    savingProjectSettings[proj.name] = false;
    savingProjectSettings = savingProjectSettings;
  }

  async function changeRuntimeVersion(proj: ProjectInfo, version: string) {
    savingProjectSettings[proj.name] = true;
    savingProjectSettings = savingProjectSettings;
    try {
      await SetProjectRuntimeVersion(proj.name, version);
      await loadProjects();
    } catch (e: any) {
      errorMessage = `${proj.name}: ${e?.message || e}`;
    }
    savingProjectSettings[proj.name] = false;
    savingProjectSettings = savingProjectSettings;
  }

  async function changeWebserver(proj: ProjectInfo, ws: string) {
    savingProjectSettings[proj.name] = true;
    savingProjectSettings = savingProjectSettings;
    try {
      await SetProjectWebserver(proj.name, ws);
      await loadProjects();
    } catch (e: any) {
      errorMessage = `${proj.name}: ${e?.message || e}`;
    }
    savingProjectSettings[proj.name] = false;
    savingProjectSettings = savingProjectSettings;
  }

  // Per-project public hostname edits (saved on blur / Enter)
  let hostnameEdits: Record<string, string> = {};

  async function savePublicHostname(proj: ProjectInfo) {
    const value = (hostnameEdits[proj.name] ?? proj.publicHostname ?? '').trim();
    if (value === (proj.publicHostname || '')) return;
    savingProjectSettings[proj.name] = true;
    savingProjectSettings = savingProjectSettings;
    try {
      await SetProjectPublicHostname(proj.name, value);
      await loadProjects();
    } catch (e: any) {
      errorMessage = `${proj.name}: ${e?.message || e}`;
    }
    savingProjectSettings[proj.name] = false;
    savingProjectSettings = savingProjectSettings;
  }

  // Add dialog: port override for app-server projects
  let newPort = 0;

  // --- Wizard state ---
  type WizardStep = 'choose-method' | 'new-project' | 'clone' | 'progress';
  let showWizard = false;
  let wizardStep: WizardStep = 'choose-method';
  let templates: TemplateInfo[] = [];
  let selectedTemplate: TemplateInfo | null = null;
  let newProjectName = '';
  let parentFolder = '';
  let wizardDomain = '';
  let cloneURL = '';
  let cloneName = '';
  let cloneParentFolder = '';
  let cloneDomain = '';
  let progressPercent = -1;
  let progressMessage = '';
  let progressLog: string[] = [];
  let progressError = '';
  let progressDone = false;
  let gitInstalled = false;

  function openWizard() {
    showWizard = true;
    wizardStep = 'choose-method';
    selectedTemplate = null;
    newProjectName = '';
    parentFolder = '';
    wizardDomain = '';
    cloneURL = '';
    cloneName = '';
    cloneParentFolder = '';
    // Default new/cloned projects into <data>/projects; the user can still browse elsewhere.
    GetDefaultProjectsDir().then(dir => {
      if (!parentFolder) parentFolder = dir;
      if (!cloneParentFolder) cloneParentFolder = dir;
    }).catch(() => {});
    cloneDomain = '';
    progressPercent = -1;
    progressMessage = '';
    progressLog = [];
    progressError = '';
    progressDone = false;
  }

  function closeWizard() {
    showWizard = false;
    if (progressDone && !progressError) {
      loadProjects();
    }
  }

  async function goToNewProject() {
    try {
      templates = await GetAvailableTemplates() || [];
    } catch (e) {
      templates = [];
    }
    wizardStep = 'new-project';
  }

  async function goToClone() {
    try {
      gitInstalled = await IsGitInstalled();
    } catch {
      gitInstalled = false;
    }
    wizardStep = 'clone';
  }

  function goToExisting() {
    showWizard = false;
    openAddDialog();
  }

  function selectTemplate(tmpl: TemplateInfo) {
    if (!tmpl.available) return;
    selectedTemplate = tmpl;
    // Auto-generate domain from project name
    updateWizardDomain();
  }

  function updateWizardDomain() {
    if (newProjectName) {
      wizardDomain = newProjectName.toLowerCase().replace(/[^a-z0-9-]/g, '-') + '.test';
    }
  }

  async function browseParentFolder() {
    try {
      const folder = await SelectParentFolder();
      if (folder) parentFolder = folder;
    } catch (e) {
      errorMessage = String(e);
    }
  }

  async function browseCloneParentFolder() {
    try {
      const folder = await SelectParentFolder();
      if (folder) cloneParentFolder = folder;
    } catch (e) {
      errorMessage = String(e);
    }
  }

  function updateCloneDomain() {
    const name = cloneName || deriveNameFromURL(cloneURL);
    if (name) {
      cloneDomain = name.toLowerCase().replace(/[^a-z0-9-]/g, '-') + '.test';
    }
  }

  function deriveNameFromURL(url: string): string {
    if (!url) return '';
    const parts = url.replace(/\/$/, '').split('/');
    return parts[parts.length - 1]?.replace(/\.git$/, '') || '';
  }

  async function startScaffold() {
    if (!selectedTemplate || !newProjectName || !parentFolder) return;
    wizardStep = 'progress';
    progressPercent = -1;
    progressMessage = $t('projects.creating');
    progressLog = [];
    progressError = '';
    progressDone = false;

    try {
      await ScaffoldNewProject(selectedTemplate.id, parentFolder, newProjectName, wizardDomain);
    } catch (e) {
      progressError = String(e);
      progressDone = true;
    }
  }

  async function startClone() {
    if (!cloneURL || !cloneParentFolder) return;
    wizardStep = 'progress';
    progressPercent = -1;
    progressMessage = $t('projects.cloning');
    progressLog = [];
    progressError = '';
    progressDone = false;

    try {
      await CloneGitProject(cloneURL, cloneParentFolder, cloneName, cloneDomain);
    } catch (e) {
      progressError = String(e);
      progressDone = true;
    }
  }

  $: templatesByCategory = {
    php: templates.filter(t => t.category === 'php'),
    node: templates.filter(t => t.category === 'node'),
    other: templates.filter(t => !['php', 'node'].includes(t.category)),
  };

  const runtimeDisplayName: Record<string, string> = {
    php: 'PHP', node: 'Node', go: 'Go', rust: 'Rust', python: 'Python',
  };

  function templateVersionLabel(tmpl: TemplateInfo): string {
    if (!tmpl.runtimeVersion) return '';
    const name = runtimeDisplayName[tmpl.requiredRuntime] || $runtimeLabel(tmpl.requiredRuntime);
    return `${name} ${tmpl.runtimeVersion}`;
  }

  function templateColor(id: string): string {
    const map: Record<string, string> = {
      laravel: 'text-red-500', symfony: 'text-slate-500', wordpress: 'text-blue-500', php: 'text-indigo-500',
      nextjs: 'text-slate-400', nuxt: 'text-green-500', vue: 'text-emerald-500', react: 'text-cyan-500',
      svelte: 'text-orange-500', angular: 'text-red-600',
      go: 'text-sky-500', rust: 'text-orange-600', django: 'text-green-600', static: 'text-gray-500',
      kemal: 'text-zinc-500',
    };
    return map[id] || 'text-slate-400';
  }

  // --- Existing functions ---

  // .env values still pointing at localhost (per project) — the #1 reason a
  // domain "redirects to 127.0.0.1:8000".
  let envHints: Record<string, { key: string; value: string }[]> = {};

  async function loadProjects() {
    loading = true;
    try {
      projects = await ListProjects() || [];
      const next: typeof envHints = {};
      await Promise.all(projects.map(async p => {
        if (!p.domain) return;
        try { const h = await GetProjectEnvHints(p.name); if (h && h.length) next[p.name] = h; } catch { /* ignore */ }
      }));
      envHints = next;
    } catch (e) {
      console.error('Failed to load projects:', e);
      projects = [];
    }
    loading = false;
  }

  async function openAddDialog() {
    try {
      const folder = await SelectProjectFolder();
      if (!folder) return;

      newPath = folder;
      const base = folder.replace(/[\\/]+$/, '').split(/[\\/]/).pop() || 'project';
      newDomain = (base.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '') || 'project') + '.test';
      detectedFramework = await DetectFramework(folder);
      // Set default port for app-server frameworks
      const defaultPorts: Record<string, number> = {
        'Next.js': 3000, 'Nuxt': 3000,
        'Vue': 5173, 'React': 5173, 'Svelte': 5173, 'Angular': 5173,
        'Django': 8000, 'Python': 8000,
        'Go': 8080, 'Rust': 8080,
      };
      newPort = defaultPorts[detectedFramework] || 0;
      showAddDialog = true;
    } catch (e) {
      errorMessage = String(e);
    }
  }

  async function confirmAdd() {
    if (!newPath) return;
    adding = true;
    errorMessage = '';
    try {
      const result = await AddProject(newPath, newDomain);
      // If user set a custom port, save it
      if (newPort > 0 && result?.name) {
        await SetProjectPort(result.name, newPort);
      }
      showAddDialog = false;
      newPath = '';
      newDomain = '';
      detectedFramework = '';
      newPort = 0;
      await loadProjects();
    } catch (e) {
      errorMessage = String(e);
    }
    adding = false;
  }

  // Removal asks first (ConfirmDialog at the bottom of the page).
  let pendingRemove: string | null = null;
  function askRemoveProject(name: string) { pendingRemove = name; }
  async function confirmRemoveProject() {
    const name = pendingRemove;
    if (!name) return;
    await removeProject(name);
    pendingRemove = null;
  }

  async function removeProject(name: string) {
    errorMessage = '';
    busyProject = name;
    try {
      if (devServerStates[name]?.running) {
        await stopDevServer(name);
      }
      if (tunnelStates[name]?.running) {
        await stopProjectTunnel(name);
      }
      await RemoveProject(name);
      await loadProjects();
    } catch (e) {
      errorMessage = String(e);
    }
    busyProject = '';
  }

  async function setupDomain(name: string) {
    errorMessage = '';
    busyProject = name;
    try {
      await SetupProjectDomain(name);
      await loadProjects();
    } catch (e) {
      errorMessage = String(e);
    }
    busyProject = '';
  }

  async function toggleSSL(name: string, enable: boolean) {
    errorMessage = '';
    sslBusy[name] = true;
    sslBusy = { ...sslBusy };
    try {
      await ToggleProjectSSL(name, enable);
      await loadProjects();
    } catch (e) {
      errorMessage = String(e);
    }
    delete sslBusy[name];
    sslBusy = { ...sslBusy };
  }

  async function openVhostConfig(name: string) {
    errorMessage = '';
    try {
      const path = await GetProjectVhostPath(name);
      if (path) {
        await OpenFileInEditor(path);
      } else {
        errorMessage = $t('projects.noVhostFile');
      }
    } catch (e) {
      errorMessage = String(e);
    }
  }

  async function openTerminal(name: string) {
    errorMessage = '';
    try { await OpenProjectTerminal(name); } catch (e: any) { errorMessage = `${name}: ${e?.message || e}`; }
  }

  function openFolder(path: string) {
    OpenProjectFolder(path);
  }

  async function openBrowser(project: ProjectInfo) {
    const devPort = devServerStates[project.name]?.port || project.port;
    if (isAppServer(project.framework)) {
      // App-server projects are reachable on their .test domain only through
      // the front-door proxy (which forwards to the dev-server port). Prefer the
      // domain when that path is available; otherwise fall back to localhost.
      let proxyUp = false;
      if (project.domain && project.hostsRegistered) {
        try { proxyUp = (await GetProxyStatus())?.running === true; } catch { proxyUp = false; }
      }
      if (proxyUp) {
        OpenInBrowser(`${project.ssl ? 'https' : 'http'}://${project.domain}`);
      } else if (devPort > 0) {
        OpenInBrowser(`http://localhost:${devPort}`);
      }
    } else if (project.ssl) {
      OpenInBrowser(`https://${project.domain}`);
    } else {
      const portSuffix = webServerPort && webServerPort !== 80 ? `:${webServerPort}` : '';
      OpenInBrowser(`http://${project.domain}${portSuffix}`);
    }
  }

  // --- Tunnel Functions ---

  async function loadTunnelStatus() {
    try {
      const running = await GetRunningTunnels();
      if (running) {
        for (const [name, url] of Object.entries(running)) {
          tunnelStates[name] = { running: true, url: url || '', starting: false };
        }
        tunnelStates = { ...tunnelStates };
      }
    } catch (e) {
      console.error('Failed to load tunnel status:', e);
    }
  }

  function isAppServer(fw: string): boolean {
    const entry = catalog[fw];
    if (entry) return entry.appServer;
    return appServerFrameworks.includes(fw);
  }

  async function startProjectTunnel(proj: ProjectInfo) {
    // If the front-door proxy is running, route the tunnel through it so domain
    // mapping is consistent: cloudflared connects to the proxy's port (80) with
    // the project's Host header, and the proxy dispatches to the right backend.
    // This is what keeps two parallel tunnels (e.g. backend.test + laraveltest.test)
    // from collapsing onto the same backend default vhost.
    let port: number;
    let ssl: boolean;
    try {
      const ps = await GetProxyStatus();
      if (ps?.running) {
        port = ps.port;
        ssl = false; // HTTPS termination through the proxy lands in phase 4
      } else {
        const devPort = devServerStates[proj.name]?.port || proj.port;
        port = isAppServer(proj.framework) && devPort > 0
          ? devPort
          : (webServerPort || 80);
        ssl = isAppServer(proj.framework) ? false : proj.ssl;
      }
    } catch {
      // Defensive fallback to legacy behavior.
      const devPort = devServerStates[proj.name]?.port || proj.port;
      port = isAppServer(proj.framework) && devPort > 0
        ? devPort
        : (webServerPort || 80);
      ssl = isAppServer(proj.framework) ? false : proj.ssl;
    }
    tunnelStates[proj.name] = { running: false, url: '', starting: true };
    tunnelStates = { ...tunnelStates };
    errorMessage = '';
    try {
      await StartTunnel(port, proj.name, proj.domain, ssl);
      tunnelStates[proj.name] = { running: true, url: '', starting: false };
      tunnelStates = { ...tunnelStates };

      tunnelPollingIntervals[proj.name] = setInterval(async () => {
        const url = await GetTunnelURL(proj.name);
        if (url) {
          tunnelStates[proj.name] = { running: true, url, starting: false };
          tunnelStates = { ...tunnelStates };
          clearInterval(tunnelPollingIntervals[proj.name]);
          delete tunnelPollingIntervals[proj.name];
        }
      }, 1500);

      setTimeout(() => {
        if (tunnelPollingIntervals[proj.name]) {
          clearInterval(tunnelPollingIntervals[proj.name]);
          delete tunnelPollingIntervals[proj.name];
        }
      }, 30000);
    } catch (e) {
      errorMessage = String(e);
      delete tunnelStates[proj.name];
      tunnelStates = { ...tunnelStates };
    }
  }

  async function stopProjectTunnel(name: string) {
    errorMessage = '';
    try {
      await StopTunnel(name);
    } catch (e) {
      errorMessage = String(e);
    }
    delete tunnelStates[name];
    tunnelStates = { ...tunnelStates };
    if (tunnelPollingIntervals[name]) {
      clearInterval(tunnelPollingIntervals[name]);
      delete tunnelPollingIntervals[name];
    }
  }

  function copyTunnelURL(name: string) {
    const url = tunnelStates[name]?.url;
    if (url) {
      navigator.clipboard.writeText(url);
    }
  }

  // --- Dev Server Functions ---

  async function loadDevServerStatus() {
    try {
      const running = await GetRunningDevServers();
      if (running) {
        for (const [name, port] of Object.entries(running)) {
          devServerStates[name] = { running: true, port: port || 0, starting: false };
        }
        devServerStates = { ...devServerStates };
      }
    } catch (e) {
      console.error('Failed to load dev server status:', e);
    }
  }

  async function startDevServer(proj: ProjectInfo) {
    devServerStates[proj.name] = { running: false, port: 0, starting: true };
    devServerStates = { ...devServerStates };
    errorMessage = '';
    try {
      await StartDevServer(proj.name);
    } catch (e) {
      errorMessage = String(e);
      delete devServerStates[proj.name];
      devServerStates = { ...devServerStates };
    }
  }

  async function stopDevServer(name: string) {
    errorMessage = '';
    try {
      await StopDevServer(name);
    } catch (e) {
      errorMessage = String(e);
    }
    delete devServerStates[name];
    devServerStates = { ...devServerStates };
  }

  function frameworkColor(fw: string): string {
    const map: Record<string, string> = {
      'Laravel': 'bg-red-500/10 text-red-500 border-red-500/20',
      'WordPress': 'bg-blue-500/10 text-blue-500 border-blue-500/20',
      'Next.js': 'bg-slate-500/10 text-slate-400 border-slate-500/20',
      'Nuxt': 'bg-green-500/10 text-green-500 border-green-500/20',
      'Vue': 'bg-emerald-500/10 text-emerald-500 border-emerald-500/20',
      'React': 'bg-cyan-500/10 text-cyan-500 border-cyan-500/20',
      'Svelte': 'bg-orange-500/10 text-orange-500 border-orange-500/20',
      'Angular': 'bg-red-600/10 text-red-600 border-red-600/20',
      'Go': 'bg-sky-500/10 text-sky-500 border-sky-500/20',
      'Rust': 'bg-orange-600/10 text-orange-600 border-orange-600/20',
      'Django': 'bg-green-600/10 text-green-600 border-green-600/20',
      'Python': 'bg-yellow-500/10 text-yellow-600 border-yellow-500/20',
      'Symfony': 'bg-slate-600/10 text-slate-500 border-slate-600/20',
      'CodeIgniter': 'bg-orange-500/10 text-orange-600 border-orange-500/20',
      'Yii': 'bg-blue-600/10 text-blue-600 border-blue-600/20',
      'CakePHP': 'bg-red-500/10 text-red-600 border-red-500/20',
      'Drupal': 'bg-sky-600/10 text-sky-600 border-sky-600/20',
      'PHP': 'bg-indigo-500/10 text-indigo-500 border-indigo-500/20',
      'Lumen': 'bg-red-500/10 text-red-500 border-red-500/20',
      'Slim': 'bg-lime-500/10 text-lime-600 border-lime-500/20',
      'Laminas': 'bg-sky-500/10 text-sky-600 border-sky-500/20',
      'Joomla': 'bg-blue-500/10 text-blue-600 border-blue-500/20',
      'Magento': 'bg-orange-500/10 text-orange-600 border-orange-500/20',
      'PrestaShop': 'bg-pink-500/10 text-pink-600 border-pink-500/20',
      'NestJS': 'bg-rose-500/10 text-rose-500 border-rose-500/20',
      'Astro': 'bg-violet-500/10 text-violet-500 border-violet-500/20',
      'Remix': 'bg-slate-500/10 text-slate-400 border-slate-500/20',
      'SvelteKit': 'bg-orange-500/10 text-orange-500 border-orange-500/20',
      'Gatsby': 'bg-purple-500/10 text-purple-500 border-purple-500/20',
      'AdonisJS': 'bg-indigo-500/10 text-indigo-500 border-indigo-500/20',
      'Express': 'bg-slate-500/10 text-slate-400 border-slate-500/20',
      'Fastify': 'bg-slate-500/10 text-slate-400 border-slate-500/20',
      'Koa': 'bg-slate-500/10 text-slate-400 border-slate-500/20',
      'Hono': 'bg-orange-500/10 text-orange-500 border-orange-500/20',
      'Vite': 'bg-violet-500/10 text-violet-500 border-violet-500/20',
      'Node': 'bg-green-600/10 text-green-600 border-green-600/20',
      'FastAPI': 'bg-teal-500/10 text-teal-600 border-teal-500/20',
      'Flask': 'bg-slate-500/10 text-slate-400 border-slate-500/20',
      'Goravel': 'bg-sky-500/10 text-sky-500 border-sky-500/20',
      'Gin': 'bg-sky-500/10 text-sky-500 border-sky-500/20',
      'Kemal': 'bg-zinc-500/10 text-zinc-500 border-zinc-500/20',
      'Crystal': 'bg-neutral-500/10 text-neutral-500 border-neutral-500/20',
      'Fiber': 'bg-sky-500/10 text-sky-500 border-sky-500/20',
      'Echo': 'bg-sky-500/10 text-sky-500 border-sky-500/20',
      'Actix': 'bg-orange-600/10 text-orange-600 border-orange-600/20',
      'Axum': 'bg-orange-600/10 text-orange-600 border-orange-600/20',
      'Rocket': 'bg-orange-600/10 text-orange-600 border-orange-600/20',
      'Static': 'bg-gray-500/10 text-gray-500 border-gray-500/20',
    };
    return map[fw] || 'bg-slate-500/10 text-slate-400 border-slate-500/20';
  }

  let eventCleanups: (() => void)[] = [];

  async function fixEnvHints(proj: ProjectInfo) {
    errorMessage = '';
    try {
      await FixProjectEnvHints(proj.name);
      await loadProjects();
    } catch (e: any) {
      errorMessage = `${proj.name}: ${e?.message || e}`;
    }
  }

  onMount(async () => {
    loadCatalog();
    loadProjects();
    loadInstalledWebservers();
    loadTunnelStatus();
    loadDevServerStatus();
    try {
      webServerPort = await GetWebServerPort();
    } catch (e) {
      console.error('Failed to get web server port:', e);
    }

    // Listen for dev server events
    eventCleanups.push(EventsOn('devserver:started', (data: any) => {
      devServerStates[data.name] = { running: true, port: data.port || 0, starting: false };
      devServerStates = { ...devServerStates };
      loadProjects();
    }));
    eventCleanups.push(EventsOn('project:warning', (data: any) => {
      errorMessage = $t(data.key, ...(data.args || []));
      loadProjects();
    }));
    eventCleanups.push(EventsOn('devserver:stopped', (data: any) => {
      delete devServerStates[data.name];
      devServerStates = { ...devServerStates };
      if (data.crashed) {
        const reason = typeof data.reason === 'string' ? data.reason.trim() : '';
        errorMessage = $t('projects.serverCrashed', data.name) + (reason ? ` ${reason}` : '');
      }
    }));
    eventCleanups.push(EventsOn('devserver:error', (data: any) => {
      if (data.name) {
        delete devServerStates[data.name];
        devServerStates = { ...devServerStates };
      }
      errorMessage = data.error || 'Dev server error';
    }));

    // Listen for scaffold events
    eventCleanups.push(EventsOn('scaffold:progress', (data: any) => {
      progressPercent = data.percent ?? -1;
      progressMessage = data.message || '';
      if (data.message) {
        progressLog = [...progressLog, data.message];
      }
    }));
    eventCleanups.push(EventsOn('scaffold:complete', (data: any) => {
      progressPercent = 100;
      progressMessage = $t('projects.operationComplete');
      progressDone = true;
    }));
    eventCleanups.push(EventsOn('scaffold:error', (data: any) => {
      progressError = data.error || 'Unknown error';
      progressDone = true;
    }));

    // Listen for clone events
    eventCleanups.push(EventsOn('clone:progress', (data: any) => {
      progressPercent = data.percent ?? -1;
      progressMessage = data.message || '';
      if (data.message) {
        progressLog = [...progressLog, data.message];
      }
    }));
    eventCleanups.push(EventsOn('clone:complete', (data: any) => {
      progressPercent = 100;
      progressMessage = $t('projects.operationComplete');
      progressDone = true;
    }));
    eventCleanups.push(EventsOn('clone:error', (data: any) => {
      progressError = data.error || 'Unknown error';
      progressDone = true;
    }));
  });

  onDestroy(() => {
    for (const interval of Object.values(tunnelPollingIntervals)) {
      clearInterval(interval);
    }
    for (const cleanup of eventCleanups) {
      cleanup();
    }
  });
</script>

<div class="space-y-6">
  <div class="flex items-center justify-between">
    <div>
      <h2 class="text-2xl font-bold">{$t('projects.title')}</h2>
      <p class="text-[var(--color-text-secondary)] mt-1">{$t('projects.subtitle')}</p>
    </div>
    <button class="btn-primary" on:click={openWizard}>
      + {$t('projects.add')}
    </button>
  </div>

  {#if errorMessage}
    <div class="p-3 rounded-lg bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 text-red-700 dark:text-red-400 text-sm">
      <div class="flex items-start justify-between gap-2">
        <pre class="whitespace-pre-wrap font-mono text-xs leading-relaxed flex-1">{errorMessage}</pre>
        <button class="text-xs underline flex-shrink-0 mt-0.5" on:click={() => errorMessage = ''}>{$t('common.dismiss')}</button>
      </div>
    </div>
  {/if}

  <!-- Wizard Modal -->
  {#if showWizard}
    <div class="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
      <div class="bg-[var(--color-card)] rounded-xl border border-[var(--color-border)] shadow-2xl w-[540px] max-h-[85vh] overflow-y-auto p-6">

        <!-- Step: Choose Method -->
        {#if wizardStep === 'choose-method'}
          <h3 class="text-lg font-bold mb-4">{$t('projects.chooseMethod')}</h3>
          <div class="grid grid-cols-1 gap-3">
            <!-- Existing Project -->
            <button
              class="flex items-center gap-4 p-4 rounded-lg border border-[var(--color-border)] hover:border-primary-500 hover:bg-primary-500/5 transition-colors text-left"
              on:click={goToExisting}
            >
              <div class="w-10 h-10 rounded-lg bg-amber-500/10 flex items-center justify-center flex-shrink-0">
                <svg class="w-5 h-5 text-amber-500" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M2 6a2 2 0 012-2h5l2 2h9a2 2 0 012 2v10a2 2 0 01-2 2H4a2 2 0 01-2-2V6z" /></svg>
              </div>
              <div>
                <div class="font-bold text-sm">{$t('projects.existingProject')}</div>
                <div class="text-xs text-[var(--color-text-secondary)] mt-0.5">{$t('projects.existingProjectDesc')}</div>
              </div>
            </button>

            <!-- New Project -->
            <button
              class="flex items-center gap-4 p-4 rounded-lg border border-[var(--color-border)] hover:border-primary-500 hover:bg-primary-500/5 transition-colors text-left"
              on:click={goToNewProject}
            >
              <div class="w-10 h-10 rounded-lg bg-emerald-500/10 flex items-center justify-center flex-shrink-0">
                <svg class="w-5 h-5 text-emerald-500" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/></svg>
              </div>
              <div>
                <div class="font-bold text-sm">{$t('projects.newProject')}</div>
                <div class="text-xs text-[var(--color-text-secondary)] mt-0.5">{$t('projects.newProjectDesc')}</div>
              </div>
            </button>

            <!-- Clone -->
            <button
              class="flex items-center gap-4 p-4 rounded-lg border border-[var(--color-border)] hover:border-primary-500 hover:bg-primary-500/5 transition-colors text-left"
              on:click={goToClone}
            >
              <div class="w-10 h-10 rounded-lg bg-purple-500/10 flex items-center justify-center flex-shrink-0">
                <svg class="w-5 h-5 text-purple-500" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M16 17l5-5-5-5M8 7l-5 5 5 5"/></svg>
              </div>
              <div>
                <div class="font-bold text-sm">{$t('projects.cloneRepo')}</div>
                <div class="text-xs text-[var(--color-text-secondary)] mt-0.5">{$t('projects.cloneRepoDesc')}</div>
              </div>
            </button>
          </div>
          <div class="flex justify-end mt-5">
            <button class="btn-secondary" on:click={closeWizard}>{$t('common.cancel')}</button>
          </div>

        <!-- Step: New Project -->
        {:else if wizardStep === 'new-project'}
          <div class="flex items-center gap-2 mb-4">
            <button class="text-[var(--color-text-secondary)] hover:text-[var(--color-text-primary)]" on:click={() => wizardStep = 'choose-method'}>
              <svg class="w-5 h-5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="15 18 9 12 15 6"/></svg>
            </button>
            <h3 class="text-lg font-bold">{$t('projects.newProject')}</h3>
          </div>

          <!-- Template Selection -->
          <div class="mb-4">
            <label class="block text-xs font-bold text-[var(--color-text-secondary)] uppercase tracking-wide mb-2">{$t('projects.selectTemplate')}</label>

            {#if templatesByCategory.php.length > 0}
              <div class="mb-3">
                <span class="text-[10px] font-bold text-[var(--color-text-secondary)] uppercase">{$t('projects.categoryPhp')}</span>
                <div class="grid grid-cols-4 gap-2 mt-1">
                  {#each templatesByCategory.php as tmpl}
                    <button
                      class="p-2 rounded-lg border text-center transition-all {selectedTemplate?.id === tmpl.id ? 'border-primary-500 bg-primary-500/10 ring-1 ring-primary-500/30' : 'border-[var(--color-border)] hover:border-primary-500/50'} {!tmpl.available ? 'opacity-40 cursor-not-allowed' : ''}"
                      on:click={() => selectTemplate(tmpl)}
                      disabled={!tmpl.available}
                      title={!tmpl.available ? (tmpl.requiresTool === 'composer' ? $t('projects.requiresComposer') : $t('projects.templateNotAvailable')) : tmpl.name}
                    >
                      <span class="text-xs font-bold {templateColor(tmpl.id)}">{tmpl.name}</span>
                      {#if tmpl.runtimeVersion}
                        <span class="block text-[9px] text-[var(--color-text-secondary)] mt-0.5">{templateVersionLabel(tmpl)}</span>
                      {/if}
                    </button>
                  {/each}
                </div>
              </div>
            {/if}

            {#if templatesByCategory.node.length > 0}
              <div class="mb-3">
                <span class="text-[10px] font-bold text-[var(--color-text-secondary)] uppercase">{$t('projects.categoryNode')}</span>
                <div class="grid grid-cols-4 gap-2 mt-1">
                  {#each templatesByCategory.node as tmpl}
                    <button
                      class="p-2 rounded-lg border text-center transition-all {selectedTemplate?.id === tmpl.id ? 'border-primary-500 bg-primary-500/10 ring-1 ring-primary-500/30' : 'border-[var(--color-border)] hover:border-primary-500/50'} {!tmpl.available ? 'opacity-40 cursor-not-allowed' : ''}"
                      on:click={() => selectTemplate(tmpl)}
                      disabled={!tmpl.available}
                      title={!tmpl.available ? $t('projects.templateNotAvailable') : tmpl.name}
                    >
                      <span class="text-xs font-bold {templateColor(tmpl.id)}">{tmpl.name}</span>
                      {#if tmpl.runtimeVersion}
                        <span class="block text-[9px] text-[var(--color-text-secondary)] mt-0.5">{templateVersionLabel(tmpl)}</span>
                      {/if}
                    </button>
                  {/each}
                </div>
              </div>
            {/if}

            {#if templatesByCategory.other.length > 0}
              <div class="mb-3">
                <span class="text-[10px] font-bold text-[var(--color-text-secondary)] uppercase">{$t('projects.categoryOther')}</span>
                <div class="grid grid-cols-4 gap-2 mt-1">
                  {#each templatesByCategory.other as tmpl}
                    <button
                      class="p-2 rounded-lg border text-center transition-all {selectedTemplate?.id === tmpl.id ? 'border-primary-500 bg-primary-500/10 ring-1 ring-primary-500/30' : 'border-[var(--color-border)] hover:border-primary-500/50'} {!tmpl.available ? 'opacity-40 cursor-not-allowed' : ''}"
                      on:click={() => selectTemplate(tmpl)}
                      disabled={!tmpl.available}
                      title={!tmpl.available ? $t('projects.templateNotAvailable') : tmpl.name}
                    >
                      <span class="text-xs font-bold {templateColor(tmpl.id)}">{tmpl.name}</span>
                      {#if tmpl.runtimeVersion}
                        <span class="block text-[9px] text-[var(--color-text-secondary)] mt-0.5">{templateVersionLabel(tmpl)}</span>
                      {/if}
                    </button>
                  {/each}
                </div>
              </div>
            {/if}
          </div>

          <!-- Project Name -->
          <div class="mb-3">
            <label for="wizard-name" class="block text-sm font-medium mb-1">{$t('projects.projectName')}</label>
            <input
              id="wizard-name"
              type="text"
              bind:value={newProjectName}
              on:input={updateWizardDomain}
              placeholder={$t('projects.projectNamePlaceholder')}
              class="w-full px-3 py-2 rounded-lg border border-[var(--color-border)] bg-[var(--color-bg)] text-sm font-mono focus:outline-none focus:ring-2 focus:ring-primary-500/50"
            />
          </div>

          <!-- Parent Folder -->
          <div class="mb-3">
            <label class="block text-sm font-medium mb-1">{$t('projects.parentFolder')}</label>
            <div class="flex gap-2">
              <div class="flex-1 px-3 py-2 rounded-lg border border-[var(--color-border)] bg-[var(--color-bg)] text-xs font-mono truncate min-h-[36px] flex items-center">
                {parentFolder || '...'}
              </div>
              <button class="btn-secondary text-sm px-3" on:click={browseParentFolder}>{$t('projects.browse')}</button>
            </div>
          </div>

          <!-- Domain -->
          <div class="mb-3">
            <label for="wizard-domain" class="block text-sm font-medium mb-1">{$t('projects.domainLabel')}</label>
            <input
              id="wizard-domain"
              type="text"
              bind:value={wizardDomain}
              placeholder={$t('projects.domainPlaceholder')}
              class="w-full px-3 py-2 rounded-lg border border-[var(--color-border)] bg-[var(--color-bg)] text-sm font-mono focus:outline-none focus:ring-2 focus:ring-primary-500/50"
            />
          </div>

          <div class="flex justify-end gap-2 mt-5">
            <button class="btn-secondary" on:click={closeWizard}>{$t('common.cancel')}</button>
            <button
              class="btn-primary"
              on:click={startScaffold}
              disabled={!selectedTemplate || !newProjectName || !parentFolder}
            >
              {$t('projects.create')}
            </button>
          </div>

        <!-- Step: Clone -->
        {:else if wizardStep === 'clone'}
          <div class="flex items-center gap-2 mb-4">
            <button class="text-[var(--color-text-secondary)] hover:text-[var(--color-text-primary)]" on:click={() => wizardStep = 'choose-method'}>
              <svg class="w-5 h-5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="15 18 9 12 15 6"/></svg>
            </button>
            <h3 class="text-lg font-bold">{$t('projects.cloneRepo')}</h3>
          </div>

          {#if !gitInstalled}
            <div class="p-3 rounded-lg bg-amber-50 dark:bg-amber-900/20 border border-amber-200 dark:border-amber-800 text-amber-700 dark:text-amber-400 text-sm mb-4">
              {$t('projects.gitNotInstalled')}
            </div>
          {/if}

          <!-- Git URL -->
          <div class="mb-3">
            <label for="clone-url" class="block text-sm font-medium mb-1">{$t('projects.gitUrl')}</label>
            <input
              id="clone-url"
              type="text"
              bind:value={cloneURL}
              on:input={updateCloneDomain}
              placeholder={$t('projects.gitUrlPlaceholder')}
              class="w-full px-3 py-2 rounded-lg border border-[var(--color-border)] bg-[var(--color-bg)] text-sm font-mono focus:outline-none focus:ring-2 focus:ring-primary-500/50"
              disabled={!gitInstalled}
            />
          </div>

          <!-- Project Name (optional) -->
          <div class="mb-3">
            <label for="clone-name" class="block text-sm font-medium mb-1">{$t('projects.projectName')} <span class="text-[var(--color-text-secondary)] font-normal">(opsiyonel)</span></label>
            <input
              id="clone-name"
              type="text"
              bind:value={cloneName}
              on:input={updateCloneDomain}
              placeholder={deriveNameFromURL(cloneURL) || $t('projects.projectNamePlaceholder')}
              class="w-full px-3 py-2 rounded-lg border border-[var(--color-border)] bg-[var(--color-bg)] text-sm font-mono focus:outline-none focus:ring-2 focus:ring-primary-500/50"
              disabled={!gitInstalled}
            />
          </div>

          <!-- Parent Folder -->
          <div class="mb-3">
            <label class="block text-sm font-medium mb-1">{$t('projects.parentFolder')}</label>
            <div class="flex gap-2">
              <div class="flex-1 px-3 py-2 rounded-lg border border-[var(--color-border)] bg-[var(--color-bg)] text-xs font-mono truncate min-h-[36px] flex items-center">
                {cloneParentFolder || '...'}
              </div>
              <button class="btn-secondary text-sm px-3" on:click={browseCloneParentFolder} disabled={!gitInstalled}>{$t('projects.browse')}</button>
            </div>
          </div>

          <!-- Domain -->
          <div class="mb-3">
            <label for="clone-domain" class="block text-sm font-medium mb-1">{$t('projects.domainLabel')}</label>
            <input
              id="clone-domain"
              type="text"
              bind:value={cloneDomain}
              placeholder={$t('projects.domainPlaceholder')}
              class="w-full px-3 py-2 rounded-lg border border-[var(--color-border)] bg-[var(--color-bg)] text-sm font-mono focus:outline-none focus:ring-2 focus:ring-primary-500/50"
              disabled={!gitInstalled}
            />
          </div>

          <div class="flex justify-end gap-2 mt-5">
            <button class="btn-secondary" on:click={closeWizard}>{$t('common.cancel')}</button>
            <button
              class="btn-primary"
              on:click={startClone}
              disabled={!gitInstalled || !cloneURL || !cloneParentFolder}
            >
              {$t('projects.clone')}
            </button>
          </div>

        <!-- Step: Progress -->
        {:else if wizardStep === 'progress'}
          <h3 class="text-lg font-bold mb-4">
            {#if progressDone && !progressError}
              {$t('projects.operationComplete')}
            {:else if progressError}
              {$t('projects.operationFailed')}
            {:else}
              {$t('projects.creating')}
            {/if}
          </h3>

          <div class="mb-4">
            <ProgressBar percent={progressPercent} message={progressMessage} />
          </div>

          {#if progressError}
            <div class="p-3 rounded-lg bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 text-red-700 dark:text-red-400 text-sm mb-4">
              <pre class="whitespace-pre-wrap font-mono text-xs">{progressError}</pre>
            </div>
          {/if}

          <!-- Log output -->
          {#if progressLog.length > 0}
            <div class="bg-[var(--color-bg)] border border-[var(--color-border)] rounded-lg p-3 max-h-40 overflow-y-auto mb-4">
              {#each progressLog.slice(-20) as line}
                <div class="text-[10px] font-mono text-[var(--color-text-secondary)] leading-relaxed">{line}</div>
              {/each}
            </div>
          {/if}

          {#if progressDone}
            <div class="flex justify-end">
              <button class="btn-primary" on:click={closeWizard}>{$t('projects.done')}</button>
            </div>
          {/if}
        {/if}
      </div>
    </div>
  {/if}

  <!-- Add Project Dialog (existing flow) -->
  {#if showAddDialog}
    <div class="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
      <div class="bg-[var(--color-card)] rounded-xl border border-[var(--color-border)] shadow-2xl w-[460px] p-6">
        <h3 class="text-lg font-bold mb-4">{$t('projects.existingProject')}</h3>

        <div class="mb-4">
          <label class="block text-sm font-medium mb-1.5">{$t('projects.selectFolder')}</label>
          <div class="flex items-center gap-2 px-3 py-2 rounded-lg border border-[var(--color-border)] bg-[var(--color-bg)]">
            <svg class="w-4 h-4 text-amber-500 flex-shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M2 6a2 2 0 012-2h5l2 2h9a2 2 0 012 2v10a2 2 0 01-2 2H4a2 2 0 01-2-2V6z" /></svg>
            <span class="text-xs font-mono truncate">{newPath}</span>
          </div>
        </div>

        {#if detectedFramework}
          <div class="mb-4 flex items-center gap-2">
            <span class="text-xs text-[var(--color-text-secondary)]">{$t('projects.frameworkDetected').replace('{0}', '')}</span>
            <span class="text-xs font-bold px-1.5 py-0.5 rounded border {frameworkColor(detectedFramework)}">{detectedFramework}</span>
          </div>
        {/if}

        <div class="mb-4">
          <label for="project-domain" class="block text-sm font-medium mb-1.5">{$t('projects.domainLabel')}</label>
          <input
            id="project-domain"
            type="text"
            bind:value={newDomain}
            placeholder={$t('projects.domainPlaceholder')}
            class="w-full px-3 py-2 rounded-lg border border-[var(--color-border)] bg-[var(--color-bg)] text-sm font-mono focus:outline-none focus:ring-2 focus:ring-primary-500/50"
          />
        </div>

        {#if isAppServer(detectedFramework)}
          <div class="mb-4">
            <label for="project-port" class="block text-sm font-medium mb-1.5">{$t('projects.port')}</label>
            <input
              id="project-port"
              type="number"
              bind:value={newPort}
              placeholder="3000"
              min="1"
              max="65535"
              class="w-full px-3 py-2 rounded-lg border border-[var(--color-border)] bg-[var(--color-bg)] text-sm font-mono focus:outline-none focus:ring-2 focus:ring-primary-500/50"
            />
          </div>
        {/if}

        <div class="flex justify-end gap-2 mt-6">
          <button class="btn-secondary" on:click={() => { showAddDialog = false; }}>{$t('common.cancel')}</button>
          <button
            class="btn-primary"
            on:click={confirmAdd}
            disabled={adding || !newPath}
          >
            {#if adding}
              <div class="w-3 h-3 border-2 border-white border-t-transparent rounded-full animate-spin inline-block mr-1"></div>
            {/if}
            {$t('projects.add')}
          </button>
        </div>
      </div>
    </div>
  {/if}

  {#if loading}
    <div class="text-center py-12">
      <div class="w-8 h-8 border-3 border-primary-500 border-t-transparent rounded-full animate-spin mx-auto mb-4"></div>
      <p class="text-sm text-[var(--color-text-secondary)]">{$t('common.loading')}</p>
    </div>
  {:else if projects.length > 0}
    <div class="space-y-3">
      {#each projects as proj}
        {@const isBusy = busyProject === proj.name}
        {@const ts = tunnelStates[proj.name]}
        {@const hasTunnel = ts?.running === true}
        {@const isTunnelStarting = ts?.starting === true}
        {@const isSSLBusy = sslBusy[proj.name] === true}
        {@const ds = devServerStates[proj.name]}
        {@const isDevRunning = ds?.running === true}
        {@const isDevStarting = ds?.starting === true}
        <div class="card p-4">
          <div class="flex items-center justify-between gap-4">
            <!-- Left: Project Info -->
            <div class="flex items-center gap-4 flex-1 min-w-0">
              <div class="w-10 h-10 rounded-lg bg-amber-500/10 flex items-center justify-center flex-shrink-0">
                <svg class="w-5 h-5 text-amber-500" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                  <path d="M2 6a2 2 0 012-2h5l2 2h9a2 2 0 012 2v10a2 2 0 01-2 2H4a2 2 0 01-2-2V6z" />
                </svg>
              </div>
              <div class="min-w-0">
                <div class="flex items-center gap-2">
                  <h3 class="font-bold text-base truncate">{proj.name}</h3>
                  {#if proj.framework}
                    <span class="text-[10px] font-bold px-1.5 py-0.5 rounded border uppercase {frameworkColor(proj.framework)}">{proj.framework}</span>
                  {/if}
                </div>
                <div class="flex items-center gap-3 mt-1 min-w-0">
                  <span class="text-xs text-[var(--color-text-secondary)] font-mono truncate min-w-0 flex-shrink" title={proj.path}>{proj.path}</span>
                  {#if proj.domain}
                    {#if proj.hostsRegistered}
                      <button class="text-xs font-mono text-primary-500 font-bold hover:underline whitespace-nowrap flex-shrink-0" on:click={() => openBrowser(proj)} title={$t('projects.openInBrowser')}>{proj.domain}</button>
                    {:else}
                      <button class="text-xs font-mono text-red-500 font-bold hover:underline flex items-center gap-1 whitespace-nowrap flex-shrink-0" on:click={() => setupDomain(proj.name)} title={$t('projects.domainNotRegistered')}>
                        <svg class="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><path d="M10.29 3.86L1.82 18a2 2 0 001.71 3h16.94a2 2 0 001.71-3L13.71 3.86a2 2 0 00-3.42 0z"/><line x1="12" y1="9" x2="12" y2="13"/><line x1="12" y1="17" x2="12.01" y2="17"/></svg>
                        {proj.domain}
                      </button>
                    {/if}
                  {/if}
                  {#if isAppServer(proj.framework) && proj.port > 0}
                    <span class="text-[10px] font-mono text-amber-500 font-bold whitespace-nowrap flex-shrink-0">:{proj.port}</span>
                  {/if}
                  {#if isDevRunning}
                    <span class="flex items-center gap-1 whitespace-nowrap flex-shrink-0">
                      <span class="w-1.5 h-1.5 rounded-full bg-emerald-500 animate-pulse"></span>
                      <span class="text-[10px] font-bold text-emerald-500">{$t('projects.serverRunning')}{ds?.port ? ` :${ds.port}` : ''}</span>
                    </span>
                  {:else if isDevStarting}
                    <span class="flex items-center gap-1 whitespace-nowrap flex-shrink-0">
                      <div class="w-2.5 h-2.5 border-2 border-amber-500 border-t-transparent rounded-full animate-spin"></div>
                      <span class="text-[10px] font-bold text-amber-500">{$t('projects.serverStarting')}</span>
                    </span>
                  {/if}
                </div>
              </div>
            </div>

            <!-- Right: Actions -->
            <div class="flex items-center gap-1.5 flex-shrink-0">
              {#if isBusy}
                <div class="w-4 h-4 border-2 border-primary-500 border-t-transparent rounded-full animate-spin"></div>
              {:else}
                <!-- Dev Server Play/Stop (app-server projects only) -->
                {#if isAppServer(proj.framework)}
                  {#if isDevRunning}
                    <button
                      class="btn-icon bg-red-500 text-white hover:bg-red-600 shadow-sm"
                      on:click={() => stopDevServer(proj.name)}
                      title={$t('projects.stopServer')}
                    >
                      <svg class="w-4 h-4" viewBox="0 0 24 24" fill="currentColor"><rect x="6" y="6" width="12" height="12" rx="1"/></svg>
                    </button>
                  {:else if isDevStarting}
                    <div class="w-8 h-8 flex items-center justify-center">
                      <div class="w-4 h-4 border-2 border-emerald-500 border-t-transparent rounded-full animate-spin"></div>
                    </div>
                  {:else}
                    <button
                      class="btn-icon bg-emerald-500/10 text-emerald-500 hover:bg-emerald-500/20"
                      on:click={() => startDevServer(proj)}
                      title={$t('projects.startServer')}
                    >
                      <svg class="w-4 h-4" viewBox="0 0 24 24" fill="currentColor"><polygon points="6,4 20,12 6,20"/></svg>
                    </button>
                  {/if}
                {/if}

                <!-- SSL Toggle -->
                {#if proj.domain}
                  <div class="flex items-center gap-1 mr-1" title={proj.ssl ? $t('projects.sslEnabled') : $t('projects.sslSetup')}>
                    <span class="text-[10px] font-bold text-[var(--color-text-secondary)]">SSL</span>
                    <button
                      class="relative w-8 h-4.5 rounded-full transition-colors duration-200 {proj.ssl ? 'bg-emerald-500' : 'bg-slate-300 dark:bg-slate-600'}"
                      style="min-width: 32px; height: 18px;"
                      on:click={() => toggleSSL(proj.name, !proj.ssl)}
                      disabled={isSSLBusy}
                    >
                      {#if isSSLBusy}
                        <div class="absolute inset-0 flex items-center justify-center">
                          <div class="w-3 h-3 border-2 border-white border-t-transparent rounded-full animate-spin"></div>
                        </div>
                      {:else}
                        <div
                          class="absolute top-0.5 w-3.5 h-3.5 rounded-full bg-white shadow transition-transform duration-200"
                          style="width: 14px; height: 14px; top: 2px; {proj.ssl ? 'left: 16px;' : 'left: 2px;'}"
                        ></div>
                      {/if}
                    </button>
                  </div>
                {/if}

                <!-- Open in Browser -->
                {#if proj.domain}
                  <button
                    class="btn-icon bg-primary-500/10 text-primary-500 hover:bg-primary-500/20"
                    on:click={() => openBrowser(proj)}
                    title={$t('projects.openInBrowser')}
                  >
                    <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6"/><polyline points="15 3 21 3 21 9"/><line x1="10" y1="14" x2="21" y2="3"/></svg>
                  </button>
                {/if}

                <!-- Terminal here -->
                <button
                  class="btn-icon bg-slate-200 dark:bg-slate-700 text-slate-600 dark:text-slate-300 hover:bg-slate-300 dark:hover:bg-slate-600"
                  on:click={() => openTerminal(proj.name)}
                  title={$t('projects.openTerminal')}
                >
                  <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="4 17 10 11 4 5"/><line x1="12" y1="19" x2="20" y2="19"/></svg>
                </button>

                <!-- Open Folder -->
                <button
                  class="btn-icon bg-slate-200 dark:bg-slate-700 text-slate-600 dark:text-slate-300 hover:bg-slate-300 dark:hover:bg-slate-600"
                  on:click={() => openFolder(proj.path)}
                  title={$t('projects.openFolder')}
                >
                  <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M2 6a2 2 0 012-2h5l2 2h9a2 2 0 012 2v10a2 2 0 01-2 2H4a2 2 0 01-2-2V6z" /></svg>
                </button>

                <!-- Setup Domain -->
                <button
                  class="btn-icon bg-amber-500/10 text-amber-600 dark:text-amber-400 hover:bg-amber-500/20"
                  on:click={() => setupDomain(proj.name)}
                  title={$t('projects.setupDomain')}
                >
                  <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><line x1="2" y1="12" x2="22" y2="12"/><path d="M12 2a15.3 15.3 0 0 1 4 10 15.3 15.3 0 0 1-4 10 15.3 15.3 0 0 1-4-10 15.3 15.3 0 0 1 4-10z"/></svg>
                </button>

                <!-- Edit Config (web-server vhost; dev-server projects have none) -->
                {#if proj.domain && !isAppServer(proj.framework)}
                  <button
                    class="btn-icon bg-slate-200 dark:bg-slate-700 text-slate-600 dark:text-slate-300 hover:bg-slate-300 dark:hover:bg-slate-600"
                    on:click={() => openVhostConfig(proj.name)}
                    title={$t('projects.editConfig')}
                  >
                    <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7"/><path d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z"/></svg>
                  </button>
                {/if}

                <!-- Settings (runtime / version / webserver) -->
                <button
                  class="btn-icon bg-slate-200 dark:bg-slate-700 text-slate-600 dark:text-slate-300 hover:bg-slate-300 dark:hover:bg-slate-600"
                  class:bg-primary-500={expandedProject === proj.name}
                  class:text-white={expandedProject === proj.name}
                  on:click={() => toggleSettingsPanel(proj)}
                  title={$t('projects.settings')}
                >
                  <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09a1.65 1.65 0 0 0-1-1.51 1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09a1.65 1.65 0 0 0 1.51-1 1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33h0a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51h0a1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82v0a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"/></svg>
                </button>

                <!-- Tunnel -->
                {#if hasTunnel}
                  <button
                    class="btn-icon bg-orange-500 text-white hover:bg-orange-600 shadow-sm"
                    on:click={() => stopProjectTunnel(proj.name)}
                    title={$t('tunnel.stopTunnel')}
                  >
                    <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                      <circle cx="18" cy="5" r="3"/><circle cx="6" cy="12" r="3"/><circle cx="18" cy="19" r="3"/>
                      <line x1="8.59" y1="13.51" x2="15.42" y2="17.49"/><line x1="15.41" y1="6.51" x2="8.59" y2="10.49"/>
                    </svg>
                  </button>
                {:else if isTunnelStarting}
                  <div class="w-8 h-8 flex items-center justify-center">
                    <div class="w-4 h-4 border-2 border-orange-500 border-t-transparent rounded-full animate-spin"></div>
                  </div>
                {:else}
                  <button
                    class="btn-icon bg-slate-200 dark:bg-slate-700 text-slate-600 dark:text-slate-300 hover:bg-orange-500/20 hover:text-orange-500"
                    on:click={() => startProjectTunnel(proj)}
                    title={$t('tunnel.share')}
                  >
                    <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                      <circle cx="18" cy="5" r="3"/><circle cx="6" cy="12" r="3"/><circle cx="18" cy="19" r="3"/>
                      <line x1="8.59" y1="13.51" x2="15.42" y2="17.49"/><line x1="15.41" y1="6.51" x2="8.59" y2="10.49"/>
                    </svg>
                  </button>
                {/if}

                <!-- Remove -->
                <button
                  class="btn-icon text-red-500 hover:bg-red-500/10"
                  on:click={() => askRemoveProject(proj.name)}
                  title={$t('projects.remove')}
                >
                  <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M3 6h18M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/></svg>
                </button>
              {/if}
            </div>
          </div>

          <!-- Tunnel URL bar -->
          {#if hasTunnel && !ts?.url}
            <div class="mt-3 pt-3 border-t border-[var(--color-border)] flex items-center gap-2">
              <div class="w-3 h-3 border-2 border-orange-500 border-t-transparent rounded-full animate-spin flex-shrink-0"></div>
              <span class="text-xs text-[var(--color-text-secondary)]">{$t('tunnel.waitingUrl')}</span>
            </div>
          {:else if hasTunnel && ts?.url}
            <div class="mt-3 pt-3 border-t border-[var(--color-border)] flex items-center gap-2">
              <span class="text-[10px] font-bold px-1.5 py-0.5 rounded border bg-orange-500/10 text-orange-500 border-orange-500/20 uppercase flex-shrink-0">{proj.publicHostname ? $t('tunnel.custom') : $t('tunnel.publicUrl')}</span>
              <button
                class="text-xs font-mono text-orange-500 font-bold hover:underline cursor-pointer truncate"
                on:click={() => copyTunnelURL(proj.name)}
                title={$t('tunnel.copyUrl')}
              >
                {ts.url}
              </button>
              <button
                class="text-[10px] text-slate-500 hover:text-orange-500 px-1 flex-shrink-0"
                on:click={() => copyTunnelURL(proj.name)}
                title={$t('tunnel.copyUrl')}
              >
                <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="9" y="9" width="13" height="13" rx="2" ry="2"/><path d="M5 15H4a2 2 0 01-2-2V4a2 2 0 012-2h9a2 2 0 012 2v1"/></svg>
              </button>
              <button
                class="text-[10px] text-slate-500 hover:text-orange-500 px-1 flex-shrink-0"
                on:click={() => OpenInBrowser(ts.url)}
                title="Open"
              >
                <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6"/><polyline points="15 3 21 3 21 9"/><line x1="10" y1="14" x2="21" y2="3"/></svg>
              </button>
            </div>
          {/if}

          {#if envHints[proj.name]}
            <div class="mt-3 p-3 rounded-lg bg-amber-500/5 border border-amber-500/20 text-xs">
              <p class="text-amber-600 dark:text-amber-400 font-medium">{$t('projects.envHintsTitle', proj.domain)}</p>
              <div class="font-mono text-[11px] mt-1.5 space-y-0.5 text-[var(--color-text-secondary)]">
                {#each envHints[proj.name] as h}<div>{h.key}=<span class="text-red-500">{h.value}</span></div>{/each}
              </div>
              <div class="flex items-center justify-between gap-3 mt-2">
                <p class="text-[var(--color-text-secondary)]">{$t('projects.envHintsFix', proj.domain)}</p>
                <button class="text-xs px-3 py-1.5 rounded-lg bg-amber-500 text-white hover:bg-amber-600 flex-shrink-0 font-semibold" on:click={() => fixEnvHints(proj)}>{$t('projects.envHintsFixBtn')}</button>
              </div>
            </div>
          {/if}

          <!-- Settings panel (runtime / version / webserver) -->
          {#if expandedProject === proj.name}
            {@const wsChoices = webserverChoicesFor(proj.runtime || '', installedWebservers, proj.webserver || '')}
            {@const versions = installedVersionsByRuntime[proj.runtime || ''] || []}
            {@const currentRuntime = proj.runtime || ''}
            {@const currentVersion = proj.runtimeVersion || ''}
            {@const currentWebserver = proj.webserver || ''}
            <div class="mt-3 pt-3 border-t border-[var(--color-border)] grid gap-3 {isAppServer(proj.framework) ? 'grid-cols-5' : 'grid-cols-4'}">
              {#if isAppServer(proj.framework)}
                <div>
                  <label class="block text-[10px] font-bold uppercase tracking-wider text-[var(--color-text-secondary)] mb-1.5">{$t('projects.autoStart')}</label>
                  <button
                    class="w-full px-2 py-1.5 text-xs rounded-lg border font-semibold transition-colors {proj.autoStart ? 'bg-emerald-500/10 text-emerald-500 border-emerald-500/30' : 'border-[var(--color-border)] bg-[var(--color-bg)] text-[var(--color-text-secondary)]'}"
                    on:click={() => toggleAutoStart(proj, !proj.autoStart)}
                    disabled={savingProjectSettings[proj.name]}
                    title={$t('projects.autoStartHint')}
                  >{proj.autoStart ? $t('projects.autoStartOn') : $t('projects.autoStartOff')}</button>
                </div>
              {/if}
              <div>
                <label class="block text-[10px] font-bold uppercase tracking-wider text-[var(--color-text-secondary)] mb-1.5">{$t('projects.runtime')}</label>
                <select
                  class="w-full px-2 py-1.5 text-xs rounded-lg border border-[var(--color-border)] bg-[var(--color-bg)]"
                  on:change={(e) => changeRuntime(proj, selectValue(e))}
                  disabled={savingProjectSettings[proj.name] || !!lockedRuntime(proj.framework)}
                  title={lockedRuntime(proj.framework) ? $t('projects.runtimeLocked', proj.framework) : ''}
                >
                  {#each runtimeChoicesFor(proj.framework, runtimeChoices) as rc}
                    <option value={rc.id} selected={currentRuntime === rc.id}>{rc.label}</option>
                  {/each}
                  {#if currentRuntime && currentRuntime !== 'static' && !runtimeChoices.some((rc) => rc.id === currentRuntime)}
                    <option value={currentRuntime} selected>{currentRuntime}</option>
                  {/if}
                </select>
                {#if currentRuntime && currentRuntime !== 'static' && !runtimeChoices.some((rc) => rc.id === currentRuntime)}
                  <p class="text-[10px] text-amber-500 mt-1">{$t('projects.runtimeUnmanaged', currentRuntime)}</p>
                {/if}
              </div>

              <div>
                <label class="block text-[10px] font-bold uppercase tracking-wider text-[var(--color-text-secondary)] mb-1.5">{$t('projects.runtimeVersion')}</label>
                <select
                  class="w-full px-2 py-1.5 text-xs rounded-lg border border-[var(--color-border)] bg-[var(--color-bg)]"
                  on:change={(e) => changeRuntimeVersion(proj, selectValue(e))}
                  disabled={savingProjectSettings[proj.name] || !proj.runtime || proj.runtime === 'static'}
                >
                  <option value="" selected={currentVersion === ''}>{$t('projects.versionGlobalDefault')}</option>
                  {#each versions as v}
                    <option value={v.number} selected={currentVersion === v.number}>v{v.number}</option>
                  {/each}
                </select>
              </div>

              <div>
                <label class="block text-[10px] font-bold uppercase tracking-wider text-[var(--color-text-secondary)] mb-1.5">{$t('projects.webserver')}</label>
                <select
                  class="w-full px-2 py-1.5 text-xs rounded-lg border border-[var(--color-border)] bg-[var(--color-bg)]"
                  on:change={(e) => changeWebserver(proj, selectValue(e))}
                  disabled={savingProjectSettings[proj.name]}
                >
                  {#each wsChoices as ws}
                    <option value={ws.id} selected={currentWebserver === ws.id}>{ws.label}</option>
                  {/each}
                </select>
              </div>

              <div>
                <label class="block text-[10px] font-bold uppercase tracking-wider text-[var(--color-text-secondary)] mb-1.5">{$t('projects.publicHostname')}</label>
                <input
                  type="text"
                  class="w-full px-2 py-1.5 text-xs font-mono rounded-lg border border-[var(--color-border)] bg-[var(--color-bg)]"
                  placeholder={$t('projects.publicHostnamePlaceholder')}
                  value={hostnameEdits[proj.name] ?? proj.publicHostname ?? ''}
                  on:input={(e) => hostnameEdits[proj.name] = inputValue(e)}
                  on:blur={() => savePublicHostname(proj)}
                  on:keydown={(e) => { if (e.key === 'Enter') blurTarget(e); }}
                  disabled={savingProjectSettings[proj.name]}
                  title={$t('projects.publicHostnameHint')}
                />
              </div>
            </div>
            <p class="text-[10px] text-[var(--color-text-secondary)] mt-2">{$t('projects.publicHostnameHint')}</p>
          {/if}
        </div>
      {/each}
    </div>
  {:else}
    <div class="card text-center py-12">
      <svg class="w-16 h-16 mx-auto mb-4 text-[var(--color-text-secondary)] opacity-30" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
        <path d="M2 6a2 2 0 012-2h5l2 2h9a2 2 0 012 2v10a2 2 0 01-2 2H4a2 2 0 01-2-2V6z" />
      </svg>
      <p class="text-[var(--color-text-secondary)] font-medium">{$t('projects.emptyTitle')}</p>
      <p class="text-sm text-[var(--color-text-secondary)] mt-1 opacity-60">{$t('projects.emptyHint')}</p>
      <button class="btn-primary mt-4" on:click={openWizard}>+ {$t('projects.add')}</button>
    </div>
  {/if}
</div>

<ConfirmDialog
  open={pendingRemove !== null}
  danger={true}
  busy={pendingRemove !== null && busyProject === pendingRemove}
  title={pendingRemove ? $t('projects.confirmRemoveTitle', pendingRemove) : ''}
  message={$t('projects.confirmRemoveMsg')}
  confirmLabel={$t('common.delete')}
  on:confirm={confirmRemoveProject}
  on:cancel={() => pendingRemove = null}
/>
