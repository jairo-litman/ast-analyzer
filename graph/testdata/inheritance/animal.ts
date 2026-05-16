export interface Speaker {
    speak(): string;
}

export class Animal implements Speaker {
    speak(): string {
        return "noise";
    }

    introduce(): string {
        return this.speak();
    }
}
