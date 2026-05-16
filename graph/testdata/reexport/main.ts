import { add } from "./helpers";
import { times } from "./helpers";
import { MathNS } from "./helpers";
import { Cool } from "./helpers";
import { deep } from "./nested";

function main() {
    add(1, 2);
    times(3, 4);
    MathNS.add(5, 6);
    Cool();
    deep();
}
