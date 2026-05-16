export interface Walker {
    walk(): void;
}

export interface Athlete extends Walker {
    run(): void;
}

export class Sprinter implements Athlete {
    run(): void {
        this.walk();
    }
}
