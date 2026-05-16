import { Box, makeBox } from "./types";

function inferred(): string {
    const b = new Box("hello");
    return b.show();
}

function annotated(): string {
    const b: Box = makeBox("world");
    return b.label();
}

function nested(): string {
    const outer = new Box("outer");
    function inner(): string {
        const innerBox = new Box("inner");
        return innerBox.label();
    }
    return outer.show() + inner();
}
