import { BaseService } from './base';
import { FooRepository } from './repo';

// ConcreteService inherits typed properties from BaseService through
// constructor-parameter shorthand. The body calls this.<inherited
// property>.<method>() — the dominant NestJS pattern.
export class ConcreteService extends BaseService {
    run(id: string): string {
        const found = this.fooRepository.find(id);
        this.fooRepository.save(found);
        return found;
    }

    count(): number {
        return this.barRepository.lookup();
    }
}

// Stand-alone class with an explicit field declaration (not via
// constructor shorthand). The resolver should pick this up via
// ClassDetails.Properties too.
export class WithExplicitField {
    private foo: FooRepository = new FooRepository();

    lookup(id: string): string {
        return this.foo.find(id);
    }
}
