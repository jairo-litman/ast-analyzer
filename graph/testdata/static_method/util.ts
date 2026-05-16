export class PathUtil {
    static join(parts: string[]): string {
        return parts.join('/');
    }

    static normalize(p: string): string {
        return p.replace(/\\/g, '/');
    }
}
