import { makeBox, makeBoxAsync, Box } from './box';

export function useBox(): number {
    const box = makeBox(7);
    return box.open();
}

// Renamed-import: const local should still resolve via the alias.
import { makeBox as buildBox } from './box';
export function useAliased(): number {
    const b = buildBox(3);
    return b.open();
}

// Promise<Box> — stripping generics gives "Promise" which isn't in
// the project, so this stays unresolved by design. Included as a
// negative regression check.
export async function useAsync(): Promise<number> {
    const box = await makeBoxAsync(5);
    return box.open();
}
