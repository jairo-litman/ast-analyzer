import { Todo } from "@models/todo";

export default class Storage {
    private items: Todo[];

    constructor() {
        this.items = [];
    }

    save(todo: Todo): void {
        this.items.push(todo);
    }

    findAll(): Todo[] {
        return this.items;
    }
}
