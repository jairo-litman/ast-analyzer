export class Asset {
    name: string = '';
    rename(n: string): void { this.name = n; }
}

export class Service {
    handle(): void { /* ... */ }
}

export class TestContext {
    handle(): void { /* ... */ }
}

export class SetupBag {
    ctx: TestContext = new TestContext();
    sut: Service = new Service();
    asset: Asset = new Asset();
}

export function setup(): SetupBag {
    return new SetupBag();
}

export async function setupAsync(): Promise<SetupBag> {
    return new SetupBag();
}
