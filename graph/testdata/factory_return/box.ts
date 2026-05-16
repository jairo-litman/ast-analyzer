export class Box {
    value: number;
    constructor(value: number) {
        this.value = value;
    }

    open(): number {
        return this.value;
    }
}

export function makeBox(value: number): Box {
    return new Box(value);
}

export async function makeBoxAsync(value: number): Promise<Box> {
    return new Box(value);
}
