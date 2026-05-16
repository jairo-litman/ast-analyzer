export function f1(): void { console.log('1'); }
export function f2(): void { console.log('2'); }
export function f3(): void { console.log('3'); }
export function f4(): void { console.log('4'); }
export function f5(): void { console.log('5'); }

export function fanoutCaller(): void {
    f1();
    f2();
    f3();
    f4();
    f5();
}
