// Leaf utility - no imports
export function validateString(s: string): boolean {
  return s.length > 0;
}

export function validateEmail(email: string): boolean {
  return email.includes("@");
}
