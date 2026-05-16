function memoize(_target: any, _key: string, descriptor: PropertyDescriptor) {
    return descriptor;
}

function Injectable() {
    return function (_target: any) { };
}

@Injectable()
class Service {
    @memoize
    compute(value: number): number {
        return value + 1;
    }
}
