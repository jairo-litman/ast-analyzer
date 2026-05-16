export enum Priority {
    Low,
    Medium,
    High,
}

export type TodoId = string;

export interface Identifiable {
    id: TodoId;
}

export interface TodoLike extends Identifiable {
    title: string;
    priority: Priority;
    complete(): void;
}
