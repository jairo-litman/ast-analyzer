import { Priority, TodoLike } from "@models/types";
import type { TodoId } from "@models/types";

export abstract class BaseTodo implements TodoLike {
    id: TodoId;
    title: string;
    priority: Priority;

    constructor(id: TodoId, title: string, priority: Priority) {
        this.id = id;
        this.title = title;
        this.priority = priority;
    }

    abstract complete(): void;

    describe(): string {
        return this.title + " (" + this.priority + ")";
    }
}

export class Todo extends BaseTodo {
    completed: boolean;

    constructor(id: TodoId, title: string, priority: Priority) {
        super(id, title, priority);
        this.completed = false;
    }

    complete(): void {
        this.completed = true;
    }

    summary(): string {
        return super.describe() + (this.completed ? " [done]" : "");
    }
}
