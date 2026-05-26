import { formatDate, formatName } from '../utils/format';
import { useAuth } from '../hooks/useAuth';

export function Dashboard() {
  const { user } = useAuth();
  const date = formatDate(new Date());
  return <div>{formatName('John', 'Doe')} - {date}</div>;
}
