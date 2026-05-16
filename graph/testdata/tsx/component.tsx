import { increment } from "./helper";

export function Counter(start: number) {
    const next = increment(start);
    return <button>{next}</button>;
}
