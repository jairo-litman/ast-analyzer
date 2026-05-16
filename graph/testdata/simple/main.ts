import { add } from "./helper";

function main() {
    const result = add(1, 2);
    console.log(result);
}

class Greeter {
    greet(name: string): string {
        return "hi " + name;
    }
}
