import { formatName } from '../utils/format';

export function Header({ firstName, lastName }: { firstName: string; lastName: string }) {
  return <header>{formatName(firstName, lastName)}</header>;
}
