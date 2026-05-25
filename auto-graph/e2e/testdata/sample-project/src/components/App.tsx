import { Header } from "./Header";
import { useAuth } from "../hooks/useAuth";
import type { User } from "../models/user";

export class App {
  header = new Header();
  auth = useAuth();

  render(user: User) {
    return `App: ${user.name}`;
  }
}
