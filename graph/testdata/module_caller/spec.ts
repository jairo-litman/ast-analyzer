import { renderTitle } from './target';

declare const describe: (name: string, fn: () => void) => void;
declare const it: (name: string, fn: () => void) => void;

describe('renderTitle', () => {
    it('returns the title', () => {
        const out = renderTitle('hello', {
            meta: 'extra',
        });
        console.log(out);
    });

    it('escapes special chars', () => {
        const out = renderTitle('<script>', {});
        console.log(out);
    });
});
