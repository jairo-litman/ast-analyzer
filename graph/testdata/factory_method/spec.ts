import { newContext } from './factory';

// runTest exercises the two-hop chain:
//   ctx  : TestContext   (via Fix 4 — bare-ident factory return)
//   user : User          (via Fix 5 — method-on-receiver return)
//   user.rename(...)     resolves to User.rename
export function runTest(): void {
    const ctx = newContext();
    const user = ctx.newUser('alice');
    user.rename('bob');
}

// Awaited form: const u = await ctx.newAsync(...). Promise<User>
// strips to "Promise" which isn't in the project, so u.rename stays
// unresolved by design — included as a negative regression check.
export async function runAsync(): Promise<void> {
    const ctx = newContext();
    const u = await ctx.newAsync('charlie');
    u.rename('dan');
}
