import Storage from "@services/storage";
import { makeTodo, findHighPriority } from "@services/index";
import * as Format from "@utils/format";
import type { TodoLike } from "@models/types";

function main(): void {
    const storage = new Storage();
    storage.save(makeTodo("1", "Buy milk"));
    storage.save(makeTodo("2", "Write thesis"));

    const high = findHighPriority(storage.findAll());
    console.log(Format.formatList(high));
}

main();
