import { transformA } from './cycleA';

export function transformB(input: string): string {
  if (input.length > 5) {
    return transformA(input.slice(0, 5));
  }
  return input.toLowerCase();
}
