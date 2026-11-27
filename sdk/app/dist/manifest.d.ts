export interface Manifest {
    schema: 1;
    id: string;
    slug: string;
    version: string;
    minLeafen?: string;
    name: Record<string, string>;
    description?: Record<string, string>;
    developer: ManifestDeveloper;
    icon?: string;
    category?: "work" | "connectors" | "mail" | "data" | "agents" | "public" | "games" | "personal" | "developer";
    tags?: string[];
    license?: string;
    surfaces?: ManifestSurfaces;
    data?: ManifestData[];
    personAccess?: "openedFile" | "read" | "content" | "structure" | "admin";
    network?: ManifestNetwork;
    device?: ("camera" | "clipboard-write" | "fullscreen" | "geolocation" | "microphone")[];
    realtime?: ManifestRealtime;
    kv?: ManifestKV;
    workers?: ManifestWorker[];
    schedules?: ManifestSchedule[];
    webhooks?: ManifestWebhook[];
    endpoints?: ManifestEndpoint[];
    events?: ManifestEvent[];
    actions?: ManifestAction[];
    slashItems?: ManifestSlashItem[];
    menuActions?: ManifestMenuAction[];
    propertyEditors?: ManifestPropertyEditor[];
    agent?: ManifestAgent;
    guests?: ManifestGuests;
    pwa?: ManifestPWA;
    notifications?: boolean;
    groups?: boolean;
    webAssembly?: boolean;
    scriptEval?: boolean;
    settings?: ManifestSetting[];
    credentials?: ManifestCredential[];
}
export interface ManifestDeveloper {
    name: string;
    url?: string;
    keyFingerprint?: string;
}
export interface ManifestSurfaces {
    fullPage?: ManifestEntry;
    blocks?: ManifestBlock[];
    sidePanel?: ManifestEntry;
    fileOpeners?: ManifestFileOpener[];
    importSources?: ManifestImportSource[];
}
export interface ManifestEntry {
    entry: string;
}
export interface ManifestBlock {
    key: string;
    name: Record<string, string>;
    entry: string;
    defaultHeight?: number;
}
export interface ManifestFileOpener {
    key: string;
    name: Record<string, string>;
    types: string[];
    entry: string;
    edit?: boolean;
    newFiles?: ManifestNewFile[];
}
export interface ManifestNewFile {
    type: string;
    name: Record<string, string>;
    template: string;
}
export interface ManifestImportSource {
    key: string;
    name: Record<string, string>;
    description: Record<string, string>;
    entry: string;
}
export interface ManifestData {
    key: string;
    file: string;
    locked?: boolean;
    hidden?: boolean;
    perPerson?: boolean;
}
export interface ManifestNetwork {
    direct?: ManifestDirect[];
    relay?: ManifestRelay[];
    sockets?: ManifestSocket[];
    oauth?: ManifestOAuth[];
}
export interface ManifestDirect {
    url: string;
    kinds: ("connect" | "font" | "frame" | "img" | "media")[];
}
export interface ManifestRelay {
    url: string;
    secrets?: string[];
    privateNetwork?: boolean;
    oauth?: string[];
    credentials?: string[];
}
export interface ManifestSocket {
    host: string;
    port?: number;
    tls: boolean;
    privateNetwork?: boolean;
    credentials?: string[];
}
export interface ManifestOAuth {
    key: string;
    authorizeUrl: string;
    tokenUrl: string;
    scopes?: string[];
    owner?: "person" | "app";
    authorizeParams?: Record<string, string>;
}
export interface ManifestRealtime {
    maxParticipants: number;
    webrtc?: boolean;
    maxRooms?: number;
    worker?: string;
    function?: string;
    loop?: boolean;
}
export interface ManifestKV {
    maxBytes: number;
    maxTtlSeconds: number;
}
export interface ManifestWorker {
    name: string;
    module: string;
    functions: string[];
    memoryMb?: number;
    timeoutSeconds?: number;
    longRunning?: boolean;
    run?: string;
    resident?: boolean;
}
export interface ManifestSchedule {
    key: string;
    cron: string;
    worker: string;
    function: string;
    perPerson?: boolean;
    name?: Record<string, string>;
    timeZone?: string;
}
export interface ManifestWebhook {
    key: string;
    worker: string;
    function: string;
    verify: "token" | "hmac-sha256";
    name?: Record<string, string>;
}
export interface ManifestEndpoint {
    key: string;
    name: Record<string, string>;
    worker: string;
    function: string;
    methods?: ("GET" | "HEAD" | "POST" | "PUT" | "PATCH" | "DELETE" | "OPTIONS" | "PROPFIND" | "PROPPATCH" | "MKCOL" | "COPY" | "MOVE" | "LOCK" | "UNLOCK" | "REPORT" | "MKCALENDAR" | "SEARCH" | "ACL")[];
    public?: boolean;
    fileBodies?: ("PUT" | "POST" | "PATCH")[];
    pages?: boolean;
}
export interface ManifestEvent {
    key: string;
    types: ("page.created" | "page.content_updated" | "page.properties_updated" | "page.moved" | "page.deleted" | "page.undeleted" | "page.locked" | "page.unlocked" | "comment.created" | "comment.updated" | "comment.deleted" | "data_source.schema_updated")[];
    worker: string;
    function: string;
    dataSource?: string;
    properties?: string[];
    where?: ManifestappEventCondition[];
}
export interface ManifestappEventCondition {
    property: string;
    equals?: string;
    notEquals?: string;
    empty?: boolean;
}
export interface ManifestAction {
    key: string;
    name: Record<string, string>;
    description?: Record<string, string>;
    input?: unknown;
    runAs: "person" | "app";
    worker: string;
    function: string;
    risk: "low" | "medium" | "high";
    requiresApproval?: boolean;
    externalEffect?: boolean;
    undo?: string;
    tool?: boolean;
    inputLabels?: Record<string, Record<string, string>>;
}
export interface ManifestSlashItem {
    key: string;
    name: Record<string, string>;
    keywords?: string[];
    action?: string;
    block?: string;
}
export interface ManifestMenuAction {
    key: string;
    name: Record<string, string>;
    on: string[];
    action: string;
}
export interface ManifestPropertyEditor {
    key: string;
    name: Record<string, string>;
    types: string[];
    entry: string;
    height?: number;
}
export interface ManifestAgent {
    summary?: Record<string, string>;
    instructions?: Record<string, string>;
    resources?: ManifestResource[];
    prompts?: ManifestPrompt[];
}
export interface ManifestResource {
    key: string;
    name: string | Record<string, string>;
    action: string;
}
export interface ManifestPrompt {
    key: string;
    name: string | Record<string, string>;
    template: string;
}
export interface ManifestGuests {
    rooms: string[];
    names?: Record<string, Record<string, string>>;
    resultDatabase?: string;
    maxPerRoom: number;
}
export interface ManifestPWA {
    shortName?: string;
    themeColor?: string;
    offline?: string[];
}
export interface ManifestSetting {
    key: string;
    type: string;
    name: Record<string, string>;
    help?: Record<string, string>;
    defaultPort?: number;
}
export interface ManifestCredential {
    key: string;
    name: Record<string, string>;
    help?: Record<string, string>;
    fields: ManifestCredentialField[];
}
export interface ManifestCredentialField {
    key: string;
    name: Record<string, string>;
    secret?: boolean;
}
