export class Asset {
    rename(n: string): void { /* ... */ }
}

export class Service {
    handle(): void { /* ... */ }
}

// Factory without an explicit return type but with a single
// `return new Asset(...)` — Alt-2 inference should pick Asset.
export function makeAsset() {
    return new Asset();
}

// Factory without an explicit return type that returns an object
// literal. Per-property sources:
//   asset:   new Asset()   → type Asset
//   service: new Service() → type Service
//   helper:  shorthand for `helper` (a local var typed via new)
//
// Alt-3 inference should record these so destructuring resolves.
export function setup() {
    const helper = new Service();
    return { asset: new Asset(), service: new Service(), helper };
}
