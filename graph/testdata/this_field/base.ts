import { FooRepository, BarRepository } from './repo';

export class BaseService {
    constructor(
        protected fooRepository: FooRepository,
        protected barRepository: BarRepository,
    ) {}
}
