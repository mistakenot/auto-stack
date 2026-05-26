import { fetchUser } from './userService';
import type { User } from '../types/user';

export function trackEvent(event: string, user: User): void {
  console.log(event, user.name);
}

export function trackPageView(path: string): void {
  console.log('page view', path);
}

export async function getAnalytics(): Promise<Record<string, number>> {
  return { visits: 100, users: 50 };
}
