import { fetchUser } from '../services/userService';
import type { User } from '../types/user';

export function useAuth(): { user: User | null } {
  const user = fetchUser('current');
  return { user };
}
