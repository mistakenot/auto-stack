import { useAuth } from '../hooks/useAuth';
import type { User } from '../types/user';

export function UserProfile({ user }: { user: User | null }) {
  if (!user) return null;
  return <div>{user.name}</div>;
}
