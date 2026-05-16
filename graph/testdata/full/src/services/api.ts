import { Todo } from "@models/todo";
import { Priority, TodoId } from "@models/types";

export function createTodo(id: TodoId, title: string): Todo {
    return new Todo(id, title, Priority.Medium);
}

export function findHighPriority(todos: Todo[]): Todo[] {
    const out: Todo[] = [];
    for (const todo of todos) {
        if (todo.priority === Priority.High) {
            out.push(todo);
        }
    }
    return out;
}
