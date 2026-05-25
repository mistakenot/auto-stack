// Uses dynamic import
export class AnalyticsService {
  async track(event: string) {
    const logger = await import("../utils/format");
    console.log(logger.formatDate(new Date()), event);
  }
}
