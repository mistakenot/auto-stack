import { UserService } from "../services/userService";

export function useAuth() {
  const svc = new UserService();
  return {
    login: (email: string) => svc.getUser(email),
  };
}
