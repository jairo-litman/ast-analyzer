import { add } from "./helper";

initLogging();

function outer() {
    function inner() {
        innerCall();
    }
    inner();
}

const result = add(1, 2);
console.log(result);
