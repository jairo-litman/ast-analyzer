import { Asset, Service } from "./types";

export function useAsset(input: Asset): Promise<Asset> {
    const helper = new Service();
    const out = helper.handle(input);
    return Promise.resolve(out);
}

export class Workflow extends Service {
    runner = new Service();

    run(asset: Asset): void {
        const local: Asset = asset;
        this.runner.handle(local);
    }
}
