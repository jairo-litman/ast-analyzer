/**
 * Class-level doc.
 */
export class WithDocs {
    /**
     * Doc for one.
     */
    one(): void {
        this.two();
    }

    /**
     * Doc for two.
     */
    two(): void {
        console.log('two');
    }

    /**
     * Doc for three -- elided when one is target (not in keep set).
     */
    three(): void {
        console.log('three');
    }
}

// Line-style doc for standalone.
// Second line of the same doc block.
export function standalone(): void {
    console.log('standalone');
}

/**
 * Arrow-function const with JSDoc.
 */
export const arrowConst = (input: string): number => {
    return input.length;
};

