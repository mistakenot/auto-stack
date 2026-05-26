# Context Pack

Budget: 766/600 tokens
Omitted: 355 tokens
Seeds: src/App.tsx

## Read First
1. src/App.tsx - seed file
2. src/components/UserProfile.tsx - direct runtime dependency of src/App.tsx
3. src/hooks/useAuth.ts - direct runtime dependency of src/App.tsx; direct neighbor of src/App.tsx with risk flags
4. src/pages/HomePage.tsx - direct runtime dependency of src/App.tsx; direct neighbor of src/App.tsx with risk flags

## Watch
- Changing src/App.tsx may affect src/__tests__/App.test.tsx.
- Changing src/App.tsx may affect src/index.ts.
- src/App.tsx has a side-effect import of src/utils/polyfills.ts.
- Omitted files worth fetching with more budget: src/utils/polyfills.ts (48 tokens), src/__tests__/App.test.tsx (34 tokens), src/index.ts (86 tokens).

## Files
### src/App.tsx
Role: seed. Tokens: 99.
Flags: entrypoint_like, high_fan_out, side_effect_import.

```tsx
import React from 'react';
import { useAuth } from './hooks/useAuth';
import { UserProfile } from './components/UserProfile';
import { HomePage } from './pages/HomePage';
import type { AppConfig } from './types/config';
import './utils/polyfills';

export function App() {
  const { user } = useAuth();
  return (
    <div>
      <UserProfile user={user} />
      <HomePage />
    </div>
  );
}
```

### src/components/UserProfile.tsx
Role: dependency. Tokens: 53.

```tsx
import { useAuth } from '../hooks/useAuth';
import type { User } from '../types/user';

export function UserProfile({ user }: { user: User | null }) {
  if (!user) return null;
  return <div>{user.name}</div>;
}
```

### src/hooks/useAuth.ts
Role: dependency. Tokens: 52.
Flags: high_fan_in.

```ts
import { fetchUser } from '../services/userService';
import type { User } from '../types/user';

export function useAuth(): { user: User | null } {
  const user = fetchUser('current');
  return { user };
}
```

### src/pages/HomePage.tsx
Role: dependency. Tokens: 63.
Flags: entrypoint_like.

```tsx
import { useAuth } from '../hooks/useAuth';
import { formatDate } from '../utils/format';

export function HomePage() {
  const { user } = useAuth();
  const today = formatDate(new Date());
  return <div>Welcome {user?.name}, today is {today}</div>;
}
```

## Omitted
- src/utils/polyfills.ts - direct runtime dependency of src/App.tsx; direct neighbor of src/App.tsx with risk flags, 48 tokens
- src/__tests__/App.test.tsx - direct runtime dependent of src/App.tsx; direct neighbor of src/App.tsx with risk flags, 34 tokens
- src/index.ts - direct runtime dependent of src/App.tsx; direct neighbor of src/App.tsx with risk flags, 86 tokens
- src/types/config.ts - direct neighbor of src/App.tsx with risk flags; direct type-only dependency of src/App.tsx, 36 tokens
- src/services/userService.ts - second-hop runtime dependency via src/hooks/useAuth.ts, 66 tokens
- src/utils/format.ts - second-hop runtime dependency via src/pages/HomePage.tsx, 48 tokens
- src/types/user.ts - second-hop type-only neighbor via src/hooks/useAuth.ts; second-hop type-only neighbor via src/components/UserProfile.tsx, 37 tokens
