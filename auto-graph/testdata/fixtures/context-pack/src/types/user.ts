export interface User {
  id: string;
  name: string;
  email: string;
}

export interface UserRole {
  role: string;
  permissions: string[];
}
