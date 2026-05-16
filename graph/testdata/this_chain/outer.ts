import { Mid } from './mid';

// Outer chains three levels deep: this.mid.leaf.sing(). The fields
// span two files (mid.ts → inner.ts), and outer.ts imports only
// Mid — Leaf surfaces only via Mid's `leaf: Leaf` annotation.
export class Outer {
    mid: Mid = new Mid();

    play(): string {
        return this.mid.leaf.sing();
    }
}
