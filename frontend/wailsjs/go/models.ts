export namespace config {
	
	export class CloudflareConfig {
	    apiToken: string;
	    accountId: string;
	    accountName: string;
	    zoneId: string;
	    zoneName: string;
	    tunnelId: string;
	    tunnelName: string;
	    tunnelToken: string;
	
	    static createFrom(source: any = {}) {
	        return new CloudflareConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.apiToken = source["apiToken"];
	        this.accountId = source["accountId"];
	        this.accountName = source["accountName"];
	        this.zoneId = source["zoneId"];
	        this.zoneName = source["zoneName"];
	        this.tunnelId = source["tunnelId"];
	        this.tunnelName = source["tunnelName"];
	        this.tunnelToken = source["tunnelToken"];
	    }
	}
	export class Config {
	    language: string;
	    theme: string;
	    autoStart: boolean;
	    startMinimized: boolean;
	    closeToTray: boolean;
	    dataDir: string;
	    activeRuntimes: Record<string, string>;
	    autoStartSvcs: string[];
	    proxyEnabled: boolean;
	    versionCacheHours: number;
	    phpCgiPorts: Record<string, number>;
	    cloudflare: CloudflareConfig;
	
	    static createFrom(source: any = {}) {
	        return new Config(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.language = source["language"];
	        this.theme = source["theme"];
	        this.autoStart = source["autoStart"];
	        this.startMinimized = source["startMinimized"];
	        this.closeToTray = source["closeToTray"];
	        this.dataDir = source["dataDir"];
	        this.activeRuntimes = source["activeRuntimes"];
	        this.autoStartSvcs = source["autoStartSvcs"];
	        this.proxyEnabled = source["proxyEnabled"];
	        this.versionCacheHours = source["versionCacheHours"];
	        this.phpCgiPorts = source["phpCgiPorts"];
	        this.cloudflare = this.convertValues(source["cloudflare"], CloudflareConfig);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace main {
	
	export class CloudflareVerifyResult {
	    accounts: tunnel.CFAccount[];
	    zones: tunnel.CFZone[];
	
	    static createFrom(source: any = {}) {
	        return new CloudflareVerifyResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.accounts = this.convertValues(source["accounts"], tunnel.CFAccount);
	        this.zones = this.convertValues(source["zones"], tunnel.CFZone);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ConfigFileEntry {
	    label: string;
	    path: string;
	
	    static createFrom(source: any = {}) {
	        return new ConfigFileEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.label = source["label"];
	        this.path = source["path"];
	    }
	}
	export class ConnectionEntry {
	    label: string;
	    value: string;
	
	    static createFrom(source: any = {}) {
	        return new ConnectionEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.label = source["label"];
	        this.value = source["value"];
	    }
	}
	export class MigrationNotice {
	    migrated: boolean;
	    from: string;
	    to: string;
	
	    static createFrom(source: any = {}) {
	        return new MigrationNotice(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.migrated = source["migrated"];
	        this.from = source["from"];
	        this.to = source["to"];
	    }
	}
	export class ProxyStatus {
	    installed: boolean;
	    running: boolean;
	    enabled: boolean;
	    port: number;
	
	    static createFrom(source: any = {}) {
	        return new ProxyStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.installed = source["installed"];
	        this.running = source["running"];
	        this.enabled = source["enabled"];
	        this.port = source["port"];
	    }
	}
	export class RuntimeVersionInfo {
	    number: string;
	    stable: boolean;
	    current: boolean;
	    installed: boolean;
	    updateFor: string;
	
	    static createFrom(source: any = {}) {
	        return new RuntimeVersionInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.number = source["number"];
	        this.stable = source["stable"];
	        this.current = source["current"];
	        this.installed = source["installed"];
	        this.updateFor = source["updateFor"];
	    }
	}
	export class RemoteVersionsResult {
	    versions: RuntimeVersionInfo[];
	    fromCache: boolean;
	    fetchedAt: string;
	
	    static createFrom(source: any = {}) {
	        return new RemoteVersionsResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.versions = this.convertValues(source["versions"], RuntimeVersionInfo);
	        this.fromCache = source["fromCache"];
	        this.fetchedAt = source["fetchedAt"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class WebLinkEntry {
	    label: string;
	    url: string;
	
	    static createFrom(source: any = {}) {
	        return new WebLinkEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.label = source["label"];
	        this.url = source["url"];
	    }
	}
	export class ServiceDetailInfo {
	    configFiles: ConfigFileEntry[];
	    connectionInfo: ConnectionEntry[];
	    webLinks: WebLinkEntry[];
	
	    static createFrom(source: any = {}) {
	        return new ServiceDetailInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.configFiles = this.convertValues(source["configFiles"], ConfigFileEntry);
	        this.connectionInfo = this.convertValues(source["connectionInfo"], ConnectionEntry);
	        this.webLinks = this.convertValues(source["webLinks"], WebLinkEntry);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace project {
	
	export class EnvHint {
	    key: string;
	    value: string;
	
	    static createFrom(source: any = {}) {
	        return new EnvHint(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.value = source["value"];
	    }
	}
	export class FrameworkTemplate {
	    id: string;
	    name: string;
	    category: string;
	    requiredRuntime: string;
	    requiresTool: string;
	    available: boolean;
	    runtimeVersion: string;
	
	    static createFrom(source: any = {}) {
	        return new FrameworkTemplate(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.category = source["category"];
	        this.requiredRuntime = source["requiredRuntime"];
	        this.requiresTool = source["requiresTool"];
	        this.available = source["available"];
	        this.runtimeVersion = source["runtimeVersion"];
	    }
	}
	export class Project {
	    name: string;
	    path: string;
	    domain: string;
	    framework: string;
	    ssl: boolean;
	    port: number;
	    startCommand: string;
	    runtime?: string;
	    runtimeVersion?: string;
	    webserver?: string;
	    publicHostname?: string;
	    hostsRegistered: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Project(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.path = source["path"];
	        this.domain = source["domain"];
	        this.framework = source["framework"];
	        this.ssl = source["ssl"];
	        this.port = source["port"];
	        this.startCommand = source["startCommand"];
	        this.runtime = source["runtime"];
	        this.runtimeVersion = source["runtimeVersion"];
	        this.webserver = source["webserver"];
	        this.publicHostname = source["publicHostname"];
	        this.hostsRegistered = source["hostsRegistered"];
	    }
	}

}

export namespace runtime {
	
	export class PHPCGIInstance {
	    version: string;
	    port: number;
	    pid: number;
	
	    static createFrom(source: any = {}) {
	        return new PHPCGIInstance(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.version = source["version"];
	        this.port = source["port"];
	        this.pid = source["pid"];
	    }
	}
	export class PHPExtension {
	    name: string;
	    enabled: boolean;
	    zend: boolean;
	    source: string;
	
	    static createFrom(source: any = {}) {
	        return new PHPExtension(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.enabled = source["enabled"];
	        this.zend = source["zend"];
	        this.source = source["source"];
	    }
	}
	export class PeclExtension {
	    name: string;
	    description: string;
	    installed: boolean;
	    version: string;
	    zend: boolean;
	
	    static createFrom(source: any = {}) {
	        return new PeclExtension(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.description = source["description"];
	        this.installed = source["installed"];
	        this.version = source["version"];
	        this.zend = source["zend"];
	    }
	}
	export class RuntimeUpdate {
	    installed: string;
	    latest: string;
	
	    static createFrom(source: any = {}) {
	        return new RuntimeUpdate(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.installed = source["installed"];
	        this.latest = source["latest"];
	    }
	}

}

export namespace service {
	
	export class AvailableVersion {
	    version: string;
	    label: string;
	
	    static createFrom(source: any = {}) {
	        return new AvailableVersion(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.version = source["version"];
	        this.label = source["label"];
	    }
	}
	export class PortStatus {
	    port: number;
	    available: boolean;
	    message: string;
	
	    static createFrom(source: any = {}) {
	        return new PortStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.port = source["port"];
	        this.available = source["available"];
	        this.message = source["message"];
	    }
	}

}

export namespace tools {
	
	export class ExternalTool {
	    id: string;
	    name: string;
	    path: string;
	    installed: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ExternalTool(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.path = source["path"];
	        this.installed = source["installed"];
	    }
	}

}

export namespace tunnel {
	
	export class CFAccount {
	    id: string;
	    name: string;
	
	    static createFrom(source: any = {}) {
	        return new CFAccount(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	    }
	}
	export class CFZone {
	    id: string;
	    name: string;
	
	    static createFrom(source: any = {}) {
	        return new CFZone(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	    }
	}
	export class CloudflareStatus {
	    configured: boolean;
	    accountName: string;
	    zoneName: string;
	    tunnelName: string;
	    connected: boolean;
	    routes: number;
	
	    static createFrom(source: any = {}) {
	        return new CloudflareStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.configured = source["configured"];
	        this.accountName = source["accountName"];
	        this.zoneName = source["zoneName"];
	        this.tunnelName = source["tunnelName"];
	        this.connected = source["connected"];
	        this.routes = source["routes"];
	    }
	}

}

export namespace updater {
	
	export class Release {
	    current: string;
	    latest: string;
	    available: boolean;
	    url: string;
	    assetUrl: string;
	    assetName: string;
	    notes: string;
	    publishedAt: string;
	    checkedAt: string;
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new Release(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.current = source["current"];
	        this.latest = source["latest"];
	        this.available = source["available"];
	        this.url = source["url"];
	        this.assetUrl = source["assetUrl"];
	        this.assetName = source["assetName"];
	        this.notes = source["notes"];
	        this.publishedAt = source["publishedAt"];
	        this.checkedAt = source["checkedAt"];
	        this.error = source["error"];
	    }
	}

}

