import { Todo } from "@models/todo";

export const formatTodo = (t: Todo): string => {
    return t.summary();
};

export const formatList = (todos: Todo[]): string => {
    return todos.map(formatTodo).join("\n");
};
