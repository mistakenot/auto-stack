import { useAuth } from '../hooks/useAuth';
import { formatDate } from '../utils/format';

export function HomePage() {
  const { user } = useAuth();
  const today = formatDate(new Date());
  return <div>Welcome {user?.name}, today is {today}</div>;
}
