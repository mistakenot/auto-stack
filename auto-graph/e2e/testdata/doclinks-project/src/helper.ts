// [autodoc(aabbcc02@12345678, 00000002)]
import { capitalize } from "./utils";

export function greet(name: string): string {
  return `Hello, ${capitalize(name)}!`;
}
