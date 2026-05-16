export class User {
    constructor(public name: string) {}

    rename(newName: string): void {
        this.name = newName;
    }
}

export class TestContext {
    newUser(name: string): User {
        return new User(name);
    }

    newAsync(name: string): Promise<User> {
        return Promise.resolve(new User(name));
    }
}
