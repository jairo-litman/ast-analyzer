import { setup, setupAsync } from './types';

// Shorthand destructuring from a bare-identifier factory.
export function runShorthand(): void {
    const { ctx, sut, asset } = setup();
    ctx.handle();
    sut.handle();
    asset.rename('x');
}

// Awaited destructuring — Promise<SetupBag> unwraps to SetupBag,
// then each destructured key resolves via its property type.
export async function runAwaited(): Promise<void> {
    const { ctx, asset } = await setupAsync();
    ctx.handle();
    asset.rename('y');
}

// Renaming destructure: { sourceProp: localName } — the local
// holds the type of sourceProp on the returned object.
export function runRenamed(): void {
    const { asset: a } = setup();
    a.rename('z');
}
