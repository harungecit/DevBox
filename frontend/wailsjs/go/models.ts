export namespace config {
	
	export class Config {
	    language: string;
	    theme: string;
	    autoStart: boolean;
	    dataDir: string;
	    activeRuntimes: Record<string, string>;
	    autoStartSvcs: string[];
	
	    static createFrom(source: any = {}) {
	        return new Config(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.language = source["language"];
	        this.theme = source["theme"];
	        this.autoStart = source["autoStart"];
	        this.dataDir = source["dataDir"];
	        this.activeRuntimes = source["activeRuntimes"];
	        this.autoStartSvcs = source["autoStartSvcs"];
	    }
	}

}

export namespace main {
	
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
	export class RuntimeVersionInfo {
	    number: string;
	    stable: boolean;
	    current: boolean;
	    installed: boolean;
	
	    static createFrom(source: any = {}) {
	        return new RuntimeVersionInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.number = source["number"];
	        this.stable = source["stable"];
	        this.current = source["current"];
	        this.installed = source["installed"];
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
	    }
	}

}

export namespace runtime {
	
	export class PHPExtension {
	    name: string;
	    enabled: boolean;
	
	    static createFrom(source: any = {}) {
	        return new PHPExtension(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.enabled = source["enabled"];
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

