import { makeAsset, setup } from './types';

// Alt-2: inferred return from `return new T(...)`.
export function useMake(): void {
    const a = makeAsset();
    a.rename('x');
}

// Alt-3: destructuring an inferred object-literal return.
export function useSetup(): void {
    const { asset, service, helper } = setup();
    asset.rename('a');
    service.handle();
    helper.handle();
}
