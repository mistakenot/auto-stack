import { validateEmail } from "@utils/validate";
import type { User } from "../models/user";

export class UserService {
  async getUser(email: string): Promise<User | null> {
    if (!validateEmail(email)) {
      return null;
    }
    return { id: "1", name: "Test", email };
  }
}
