import { Animal } from "./animal";

export class Dog extends Animal {
    speak(): string {
        return "woof";
    }

    bark(): string {
        return super.introduce();
    }

    nameMyself(): string {
        return this.speak();
    }
}
