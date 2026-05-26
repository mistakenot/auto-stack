import { transformB } from './cycleB';

export function transformA(input: string): string {
  if (input.length > 10) {
    return transformB(input.slice(0, 10));
  }
  return input.toUpperCase();
}
