import { validateString } from "./validate";

export function formatDate(d: Date): string {
  validateString(d.toISOString());
  return d.toISOString().split("T")[0];
}

export function formatCurrency(amount: number): string {
  return `$${amount.toFixed(2)}`;
}
