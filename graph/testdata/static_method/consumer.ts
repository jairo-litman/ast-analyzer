import { PathUtil } from './util';

export function buildPath(): string {
    const raw = PathUtil.join(['a', 'b', 'c']);
    return PathUtil.normalize(raw);
}

// Local class with a static, called from the same file.
class Counter {
    static next(): number {
        return Math.random();
    }
}

export function nextId(): number {
    return Counter.next();
}
