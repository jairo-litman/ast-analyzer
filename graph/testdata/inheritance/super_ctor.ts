export class Base {
    constructor(public name: string) {
        // base init
    }
}

export class Derived extends Base {
    constructor(name: string, public age: number) {
        super(name);
    }
}

export class GrandchildSkipMiddle extends Derived {
    // No explicit constructor — relies on JS default behavior.
}

export class ThreeDeep extends GrandchildSkipMiddle {
    constructor() {
        // Walks up past GrandchildSkipMiddle (no constructor) to
        // Derived's constructor.
        super('three');
    }
}
