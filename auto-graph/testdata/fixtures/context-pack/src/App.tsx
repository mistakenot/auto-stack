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
