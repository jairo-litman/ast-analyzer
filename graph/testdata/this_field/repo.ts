export class FooRepository {
    find(id: string): string {
        return id;
    }

    save(value: string): void {
        // no-op
    }
}

export class BarRepository {
    lookup(): number {
        return 42;
    }
}
