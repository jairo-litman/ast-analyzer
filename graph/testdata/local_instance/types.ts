export class Box {
    contents: string;

    constructor(contents: string) {
        this.contents = contents;
    }

    show(): string {
        return this.contents;
    }

    label(): string {
        return "[" + this.contents + "]";
    }
}

export function makeBox(value: string): Box {
    return new Box(value);
}
