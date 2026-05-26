export async function loadAnalytics() {
  const mod = await import('../services/analyticsService');
  return mod;
}
