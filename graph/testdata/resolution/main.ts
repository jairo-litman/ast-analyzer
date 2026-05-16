import { add, Greeter } from "./helper";
import { multiply as times } from "./helper";
import * as utils from "./helper";

function main() {
    const a = add(1, 2);
    const b = times(3, 4);
    const c = utils.subtract(5, 6);
    const g = new Greeter();
    console.log(a, b, c, g);
    local(a);
}

function local(x: number) {
    return x;
}
