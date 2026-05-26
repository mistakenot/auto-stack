import type { User } from '../types/user';

export async function fetchUser(id: string): Promise<User> {
  return { id, name: 'Test User', email: 'test@example.com' };
}

export async function updateUser(user: User): Promise<void> {
  // update user in database
}
