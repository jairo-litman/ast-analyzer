export interface Identifiable {
    id: string;
}

export interface Trackable extends Identifiable {
    updatedAt: Date;
}

export class Asset implements Trackable {
    id: string = "";
    updatedAt: Date = new Date();
    parent?: Asset;
}

export class Service {
    handle(asset: Asset): Asset {
        return asset;
    }
}

export type AssetOrService = Asset | Service;
