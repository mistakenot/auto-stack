import { formatDate } from '@/utils/format'
import { Header } from '../components/Header'

export function Dashboard() {
  return formatDate(new Date()) + Header()
}
