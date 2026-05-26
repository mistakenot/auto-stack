import { formatDate } from '@/utils/format';
import { formatDate as fd } from '../utils/format';

export function Dashboard() {
  return formatDate(new Date()) + fd(new Date());
}
